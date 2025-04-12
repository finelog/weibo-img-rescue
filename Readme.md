# Weibo Image Rescuer
weibo, a social media platform, akin to twitter (now x).
sometimes, image might be censored due to various reasons.
this tool use a technique inspired by `tombkeeper`.
basically, even if one image is being censored, an intact
cached copy might still be available from a cdn server.

## Installation
```
$ go install github.com/finelog/weibo-img-rescue
```

## Usage
just rescue image
```
$ weibo-img-rescue https://xxx.sinaimg.cn/...
```
retrieve sina cdn ips
```
$ weibo-img-rescue -freship-only https://xxx.sinaimg.cn/... > ips.txt
```
specify ips to retrieve image
```
$ cat ips.txt
1.1.1.1
2.2.2.2
...
$ cat ips.txt | weibo-img-rescue -ip-from-stdin https://xxx.sinaimg.cn/...
```
