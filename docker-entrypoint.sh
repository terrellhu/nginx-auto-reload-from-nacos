#!/bin/bash
# vim:sw=4:ts=4:et

set -e
#-s nacos-headless -p 8848 -g DEFAULT_GROUP -n ec755dec-d85f-4f40-9b20-648e6c11c2b8 &
#GNL_ARGS="-s 122.51.63.52 -p 8848 -g DEFAULT_GROUP -n a652bfd2-a96f-4a72-a415-68f0c6e89f28"
/root/GoNacosListener $GNL_ARGS &

sleep 1
for i in {1..3}
do
    if [ ! -f "/etc/nginx/nginx.conf" ]; then
        sleep 1
    else
        break
    fi
done

exec nginx -g 'daemon off;'
