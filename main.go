/*
 * Copyright 1999-2020 Alibaba Group Holding Ltd.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *      http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package main

import (
	"bytes"
	"flag"
	"fmt"
	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"github.com/nacos-group/nacos-sdk-go/vo"
	"github.com/patrickmn/go-cache"
	"gopkg.in/yaml.v2"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"
)

var (
	help bool
	file string

	tCache *cache.Cache
	cf     Conf
)

func init() {
	tCache = cache.New(0, 0)

	flag.BoolVar(&help, "h", false, "help")
	flag.StringVar(&file, "f", "", "config file")
}

type NacosFile struct {
	DataId string `yaml:"dataId"`
	Group  string `yaml:"group"`
	Path   string `yaml:"path"`
}

type Conf struct {
	Alert struct {
		Enabled bool   `yaml:"enabled"`
		Url     string `yaml:"url"`
	}
	Nacos struct {
		Ip         string      `yaml:"ip"`
		Port       uint64      `yaml:"port"`
		Namespace  string      `yaml:"namespace"`
		NacosFiles []NacosFile `yaml:"nacosFiles"`
	}
}

func (c *Conf) getConf(file string) *Conf {
	confFile, err := ioutil.ReadFile(file)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	err = yaml.Unmarshal(confFile, c)
	if err != nil {
		fmt.Println(err.Error())
		return nil
	}
	return c
}

func alertWorkwx(content string) {
	if !cf.Alert.Enabled {
		return
	}
	hostname, _ := os.Hostname()
	dataJsonStr := fmt.Sprintf(`{"msgtype": "text", "text": {"content": "%s"}}`, hostname+"\n"+content)
	resp, err := http.Post(cf.Alert.Url, "application/json", bytes.NewBuffer([]byte(dataJsonStr)))
	if err != nil {
		fmt.Println("weworkAlarm request error")
	}
	defer resp.Body.Close()
}

func checkFileIsExist(filename string) bool {
	var exist = true
	if _, err := os.Stat(filename); os.IsNotExist(err) {
		exist = false
	}
	return exist
}

func renewFile(filename string, data string) error {
	var f *os.File
	var err error
	if checkFileIsExist(filename) { //如果文件存在
		f, err = os.OpenFile(filename, os.O_WRONLY|os.O_TRUNC, 0666) //打开文件
	} else {
		f, err = os.Create(filename) //创建文件
	}
	if err != nil {
		return err
	}
	defer f.Close()
	// 写入新配置
	_, err = io.WriteString(f, data)
	if err != nil {
		return err
	}
	return nil
}

func reloadNginx() {
	// 执行nginx -t
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd := exec.Command("nginx", "-t")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		fmt.Printf("nginx -t exec err[%v] stderr:\n%s\n", err, stderr.String())
		alertWorkwx("nginx config test failed.")
		return
	}

	// nginx -s reload
	cmd = exec.Command("nginx", "-s", "reload")
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()
	if err != nil {
		fmt.Printf("nginx -s reload exec err[%v] stderr:\n%s\n", err, stderr.String())
		alertWorkwx("nginx -s reload failed.")
		return
	}
	alertWorkwx("nginx -s reload success.")
}

func main() {
	//创建监听退出chan
	c := make(chan os.Signal)
	//监听指定信号 ctrl+c kill
	signal.Notify(c, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		for s := range c {
			if s == syscall.SIGHUP || s == syscall.SIGINT || s == syscall.SIGTERM || s == syscall.SIGQUIT {
				os.Exit(0)
			}
		}
	}()

	flag.Parse()

	if help {
		flag.Usage()
		os.Exit(0)
	}

	_ = cf.getConf(file)

	// 校验参数合法
	if len(cf.Nacos.Ip) == 0 || len(cf.Nacos.Namespace) == 0 || cf.Nacos.Port == 0 || len(cf.Nacos.NacosFiles) == 0 {
		fmt.Printf("arg err. [%v]\n", cf)
		os.Exit(0)
	}

	fmt.Println(cf)

	// nacos server 配置
	sc := []constant.ServerConfig{
		*constant.NewServerConfig(
			cf.Nacos.Ip,
			cf.Nacos.Port,
			constant.WithScheme("http"),
			constant.WithContextPath("/nacos")),
	}

	// nacos 客户端配置
	cc := constant.ClientConfig{
		NamespaceId:         cf.Nacos.Namespace, //namespace id
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		LogDir:              "logs",
		CacheDir:            "cache",
		LogLevel:            "error",
	}

	// nacos客户端初始化
	client, err := clients.NewConfigClient(
		vo.NacosClientParam{
			ClientConfig:  &cc,
			ServerConfigs: sc,
		},
	)
	if err != nil {
		fmt.Printf("nacos client init err[%v]\n", err)
		os.Exit(0)
	}

	// 先拉取监听的配置文件
	for i := 0; i < len(cf.Nacos.NacosFiles); i++ {
		file, err := client.GetConfig(vo.ConfigParam{
			DataId: cf.Nacos.NacosFiles[i].DataId,
			Group:  cf.Nacos.NacosFiles[i].Group,
		})
		if err != nil {
			fmt.Printf("get config file err[%v]\n", err)
			os.Exit(0)
		}
		err = renewFile(cf.Nacos.NacosFiles[i].Path, file)
		if err != nil {
			fmt.Printf("renew config file err[%v]\n", err)
			os.Exit(0)
		}
	}

	// 缓存监听文件信息，在回调中使用
	tCache.Set("config", cf, cache.NoExpiration)

	// 监听配置文件变更
	for i := 0; i < len(cf.Nacos.NacosFiles); i++ {
		err = client.ListenConfig(vo.ConfigParam{
			DataId: cf.Nacos.NacosFiles[i].DataId,
			Group:  cf.Nacos.NacosFiles[i].Group,
			OnChange: func(namespace, group, dataId, data string) {
				d, found := tCache.Get("config")
				if found {
					cf := d.(Conf)
					for i := 0; i < len(cf.Nacos.NacosFiles); i++ {
						if dataId == cf.Nacos.NacosFiles[i].DataId {
							filename := cf.Nacos.NacosFiles[i].Path
							err = renewFile(filename, data)
							if err != nil {
								fmt.Println("renew file " + filename + "failed.")
							} else {
								reloadNginx()
							}
						}
					}
				}
			},
		})
	}

	for {
		time.Sleep(time.Second)
	}

}
