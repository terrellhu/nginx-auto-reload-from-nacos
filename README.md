# GoNacosListener

### 背景
nginx的配置文件管理，一直想找一个集中管理配置并自动重启nginx的工具，但是找了一圈都没找到。
刚好后台服务的配置都使用的nacos，于是想着把nginx配置放到nacos上也一起管理。
于是有了这个go语言的nginx的nacos配置监听工具。

### 功能
1. 监听指定的nacos配置文件，有变更则自动替换本地配置
2. 配置变更后先执行nginx -t测试配置文件正确性，然后nginx -s reload
3. 如果配置不正确，则发出企业微信告警