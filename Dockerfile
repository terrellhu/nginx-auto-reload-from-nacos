# 编译镜像
FROM golang:1.19.2 AS build

WORKDIR /project/
COPY ./ /project
RUN go env -w GOPROXY=https://goproxy.io,direct
RUN go build -o /project/build/NginxNacosListener main.go

# 运行镜像，sed -i 's/dl-cdn.alpinelinux.org/mirrors.cloud.tencent.com/g' /etc/apk/repositories
FROM nginx:1.22.1-alpine
ENV TZ Asia/Shanghai
COPY --from=build /project/build/NginxNacosListener /nginx_nacos_listener/bin/
COPY resources/config.yaml /nginx_nacos_listener/conf/
COPY resources/docker-entrypoint.sh /
# 使用带vts模块的nginx
COPY resources/nginx /usr/sbin/

# alpine动态库不兼容glibc程序，需要建个软链，使用vts版本的nginx缺少pcre库需要安装
RUN mkdir /lib64 && ln -s /lib/libc.musl-x86_64.so.1 /lib64/ld-linux-x86-64.so.2 \
    && rm -rf /etc/nginx/conf.d/default.conf \
    && sed -i 's/dl-cdn.alpinelinux.org/mirrors.cloud.tencent.com/g' /etc/apk/repositories \
    && apk add pcre

# 定义工作目录为work
WORKDIR /nginx_nacos_listener

EXPOSE 80
# 使用nginx镜像默认的脚本启动listener服务
#ENTRYPOINT ["tail", "-f", "/dev/null"]