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
	"errors"
	"flag"
	"fmt"
	"github.com/nacos-group/nacos-sdk-go/clients"
	"github.com/nacos-group/nacos-sdk-go/common/constant"
	"github.com/nacos-group/nacos-sdk-go/vo"
	"github.com/patrickmn/go-cache"
	"github.com/xen0n/go-workwx"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

type FileInfo struct {
	DataId string `json:"dataId"`
	File   string `json:"file"`
	Group  string `json:"group"`
}

type FileList struct {
	FileArr []FileInfo `json:"fileArr"`
}

var (
	help           bool
	nacosIp        string
	nacosPort      uint64
	nacosNamespace string
	files          string
	fileList       FileList
	tCache         *cache.Cache
	isAlert        bool
	workWxClient   *workwx.Workwx
	alertApp       *workwx.WorkwxApp
)

func init() {
	workWxClient = workwx.New("ww535912ccc9fb5be5")
	alertApp = workWxClient.WithApp("gk7LUzuLOsf4W3CjLw2B7eXw9iVHWoXNt86_jiYQkVw", 1000123)
	tCache = cache.New(0, 0)
	flag.BoolVar(&isAlert, "a", false, "workwx alert. default is close")
	flag.BoolVar(&help, "h", false, "help")
	flag.StringVar(&nacosIp, "s", "", "nacos server ip")
	flag.Uint64Var(&nacosPort, "p", 0, "nacos server port")
	flag.StringVar(&nacosNamespace, "n", "", "nacos namespace")
	flag.StringVar(&files, "f", "", "listen file list, format:\nDataId&Group&File#DataId&Group&File")
}

func alertWorkwx(content string) {
	if !isAlert {
		return
	}
	c := workwx.Recipient{
		PartyIDs: []string{"72"},
	}
	hostname, _ := os.Hostname()
	alertApp.SendTextMessage(&c, hostname+"\n"+content, false)
}

func parseFilesArg(arg string) error {
	fileList = FileList{}
	// 先以#切片
	f := strings.Split(arg, "#")
	if len(f) < 1 || len(f[0]) < 1 {
		return errors.New("args format err.")
	}
	// 循环处理&
	for i := 0; i < len(f); i++ {
		fi := strings.Split(f[i], "*")
		if len(fi) != 3 {
			return errors.New("args format err.")
		}
		fit := FileInfo{
			DataId: fi[0],
			File:   fi[2],
			Group:  fi[1],
		}
		fileList.FileArr = append(fileList.FileArr, fit)
	}

	return nil
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

	// 校验参数合法
	if len(nacosIp) == 0 || len(nacosNamespace) == 0 || nacosPort == 0 || len(files) == 0 {
		fmt.Printf("arg err. nacosIp=[%s] nacosPort=[%d] nacosNamespace=[%s] files[%s]\n",
			nacosIp, nacosPort, nacosNamespace, files)
		os.Exit(0)
	}

	// 解析监听文件列表参数
	err := parseFilesArg(files)
	if err != nil {
		fmt.Println(err)
		os.Exit(0)
	}

	// nacos server 配置
	sc := []constant.ServerConfig{
		*constant.NewServerConfig(
			nacosIp,
			nacosPort,
			constant.WithScheme("http"),
			constant.WithContextPath("/nacos")),
	}

	// nacos 客户端配置
	cc := constant.ClientConfig{
		NamespaceId:         nacosNamespace, //namespace id
		TimeoutMs:           5000,
		NotLoadCacheAtStart: true,
		LogDir:              "logs",
		CacheDir:            "cache",
		RotateTime:          "1h",
		MaxAge:              3,
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
	for i := 0; i < len(fileList.FileArr); i++ {
		file, err := client.GetConfig(vo.ConfigParam{
			DataId: fileList.FileArr[i].DataId,
			Group:  fileList.FileArr[i].Group,
		})
		if err != nil {
			fmt.Printf("get config file err[%v]\n", err)
			os.Exit(0)
		}
		err = renewFile(fileList.FileArr[i].File, file)
		if err != nil {
			fmt.Printf("renew config file err[%v]\n", err)
			os.Exit(0)
		}
	}

	// 缓存监听文件信息
	tCache.Set("fileList", fileList, cache.NoExpiration)

	// 监听配置文件变更
	for i := 0; i < len(fileList.FileArr); i++ {
		err = client.ListenConfig(vo.ConfigParam{
			DataId: fileList.FileArr[i].DataId,
			Group:  fileList.FileArr[i].Group,
			OnChange: func(namespace, group, dataId, data string) {
				d, found := tCache.Get("fileList")
				if found {
					fileList := d.(FileList)
					for i := 0; i < len(fileList.FileArr); i++ {
						if dataId == fileList.FileArr[i].DataId {
							filename := fileList.FileArr[i].File
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
