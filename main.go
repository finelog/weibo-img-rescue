package main

import (
	"bufio"
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

var output string
var verbose bool
var yqceUser string
var yqceApikey string
var ipfromStdin bool
var freshipOnly bool
var ipchan = make(chan string, 64)

func init() {
	flag.BoolVar(&verbose, "verbose", false, "verbose output")
	flag.BoolVar(&freshipOnly, "freship-only", false, "only fresh the ips of domain, don't download image")
	flag.BoolVar(&ipfromStdin, "ip-from-stdin", false, "read ips from stdin")
	flag.StringVar(&output, "output", "", "optional, the output image file name, default to file name of the url")
	flag.StringVar(&yqceUser, "yqce-user", "", "optional, 17ce user name to fetch cdn ips")
	flag.StringVar(&yqceApikey, "yqce-apikey", "", "optional, 17ce api key to fetch cdn ips")
	flag.Parse()
}

func main() {
	if len(flag.Args()) < 1 {
		println("please specify the image url argument")
		flag.Usage()
		return
	}
	if ipfromStdin && freshipOnly {
		println("arguments are not making sense")
		flag.Usage()
		return
	}

	imgurl := flag.Args()[0]
	img, err := url.Parse(imgurl)
	if err != nil {
		println("malformed image url:" + imgurl)
		return
	}

	if freshipOnly {
		freships(img)
		return
	}

	if ipfromStdin {
		go func() {
			scan := bufio.NewScanner(os.Stdin)
			for scan.Scan() {
				ipchan <- strings.TrimSpace(scan.Text())
			}
			close(ipchan)
		}()
	} else {
		go func() {
			freships(img)
		}()
	}

	if output == "" {
		if pos := strings.LastIndexByte(img.Path, '/'); pos >= 0 {
			output = img.Path[pos+1:]
		} else {
			output = img.Path
		}
	}
	port := img.Port()
	if port == "" && img.Scheme == "https" {
		port = ":443"
	} else if port == "" {
		port = ":80"
	}
	var lastip string

	dialer := &net.Dialer{Timeout: 5 * time.Second}
	tp := http.DefaultTransport.(*http.Transport)
	tp.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		return dialer.DialContext(ctx, network, lastip+port)
	}
	tp.DisableKeepAlives = true // use this to call dial for every request
	http.DefaultClient.CheckRedirect = func(
		req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}

	req, err := http.NewRequest("GET", imgurl, nil)
	if err != nil {
		panic(err)
	}
	req.Header.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36")
	req.Header.Add("accept", "image/avif,image/webp,image/apng,image/svg+xml,image/*,*/*;q=0.8")
	req.Header.Add("cache-control", "no-cache")
	req.Header.Add("pragma", "no-cache")
	req.Header.Add("referer", "https://weibo.com/")
	req.Header.Add("sec-ch-ua", `"Google Chrome";v="119", "Chromium";v="119", "Not?A_Brand";v="24"`)
	req.Header.Add("sec-ch-ua-mobile", "?0")
	req.Header.Add("sec-ch-ua-platform", "\"Windows\"")
	req.Header.Add("sec-fetch-dest", "image")
	req.Header.Add("sec-fetch-mode", "no-cors")
	req.Header.Add("sec-fetch-site", " cross-site")

	// save cursor postion, restore, erase after cursor
	scpos, rcpos, erase := "\x1B[s", "\x1B[u", "\x1B[0J"

	var seq int = -1
	do := func(ip string) bool {
		seq++
		rsp, err := http.DefaultClient.Do(req)
		if !verbose {
			print(erase + scpos + "tring..." + strconv.Itoa(seq) + rcpos)
		}
		if err != nil {
			if !verbose {
				print(erase)
			}
			println(ip + " " + err.Error())
			return false
		}
		defer rsp.Body.Close()

		if rsp.StatusCode != 200 || rsp.ContentLength == 8844 {
			if verbose {
				println(ip + " " + rsp.Status + " no image")
			}
			return false
		}

		f, err := os.Create(output)
		if err != nil {
			panic(ip + " " + err.Error())
		}
		defer f.Close()
		io.Copy(f, rsp.Body)
		if verbose {
			println(ip + " " + rsp.Status + " got image")
		} else {
			println(erase + "succeed...please check out the image:" + output)
		}
		return true
	}

	for lastip = range ipchan {
		if do(lastip) {
			return
		}
	}
	println("sorry,this image is beyond rescue.")
}

func freships(img *url.URL) {
	wsargs := url.Values{}
	if yqceUser != "" && yqceApikey != "" {
		yqceCoder(wsargs)
	} else if yqceUser != "" || yqceApikey != "" {
		println("invalid arguments for yqce")
		return
	} else {
		yqceImposter(img, wsargs)
	}

	wsheaders := http.Header{}
	wsheaders.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36")
	wsheaders.Add("accept", "*/*")
	wsheaders.Add("content-type", "application/x-www-form-urlencoded; charset=UTF-8")
	wsheaders.Add("referer", "https://17ce.com/")
	wsheaders.Add("Origin", "https://17ce.com")

	wscon, rsp, err := websocket.DefaultDialer.Dial(
		"wss://wsapi.17ce.com:8001/socket/?"+wsargs.Encode(),
		wsheaders)
	if err != nil {
		panic(err)
	}
	io.Copy(os.Stdout, rsp.Body)
	rsp.Body.Close()

	type wsrsp struct {
		Rt    int
		Error string
		Msg   string
		Txnid int
		Type  string
		Data  json.RawMessage
	}
	wsret := wsrsp{}
	if err := wscon.ReadJSON(&wsret); err != nil {
		panic(err)
	} else if wsret.Rt != 1 {
		fmt.Printf("%+v\n", wsret)
		return
	}

	wsreq := `{"txnid":1,"nodetype":[1,2],"num":2,"Url":"%s","TestType":"CDN","Host":"","TimeOut":10,"Request":"GET","NoCache":true,"Speed":0,"Cookie":"","Trace":false,"Referer":"","UserAgent":"","FollowLocation":3,"GetMD5":true,"GetResponseHeader":true,"MaxDown":1048576,"AutoDecompress":false,"type":1,"isps":[0,1,2,6,7,8,17,18,19,3,4],"pro_ids":[12,49,79,80,180,183,184,188,189,190,192,193,194,195,196,221,227,235,236,238,241,243,250,346,349,350,351,353,354,355,356,357,239,352,3,5,8,18,27,42,43,46,47,51,56,85],"areas":[0,1,2,3],"SnapShot":false,"postfield":"","PingCount":10,"PingSize":32,"SrcIP":""}`
	wsreq = fmt.Sprintf(wsreq, img.Scheme+"://"+img.Host)
	if err := wscon.WriteMessage(websocket.TextMessage, []byte(wsreq)); err != nil {
		panic(err)
	}
	type srcip struct {
		HttpCode int
		SrcIP    string
	}
	srcips := struct {
		Data []srcip
	}{Data: make([]srcip, 0, 10)}
	for {
		wsret.Rt, srcips.Data = 0, srcips.Data[:0]
		if err := wscon.ReadJSON(&wsret); err != nil {
			panic(err)
		} else if wsret.Rt != 1 {
			fmt.Printf("%v\n", wsret)
			return
		} else if wsret.Type == "TaskEnd" {
			println("task end. closing")
			wscon.Close()
			break
		} else if wsret.Type == "NewData" {
			if err := json.Unmarshal(wsret.Data, &srcips); err != nil {
				fmt.Printf("%v,%+v\n", err, wsret)
			}
			for _, ip := range srcips.Data {
				if freshipOnly {
					fmt.Println(ip.SrcIP)
				} else {
					ipchan <- ip.SrcIP
				}
			}
		}
	}
	close(ipchan)
}

func yqceCoder(wsargs url.Values) {
	ut := strconv.FormatInt(time.Now().Unix(), 10)
	keysum := md5.Sum([]byte(yqceApikey))
	sumbts := []byte(hex.EncodeToString(keysum[:])[4:23] + yqceUser + ut)
	dstbts := make([]byte, base64.StdEncoding.EncodedLen(len(sumbts)))
	base64.StdEncoding.Encode(dstbts, sumbts)
	keysum = md5.Sum(dstbts)
	hex.EncodeToString(keysum[:])

	wsargs.Add("user", yqceUser)
	wsargs.Add("code", hex.EncodeToString(keysum[:]))
	wsargs.Add("ut", ut)
}

func yqceImposter(img *url.URL, wsargs url.Values) {
	host := img.Scheme + "://" + img.Host

	form := url.Values{}
	form.Add("url", host)
	form.Add("type", "http")
	form.Add("isp", "0")

	req, err := http.NewRequest("POST", "https://17ce.com/site/checkuser", strings.NewReader(form.Encode()))
	if err != nil {
		panic(err)
	}
	clt := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   10 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}

	req.Header.Add("user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/119.0.0.0 Safari/537.36")
	req.Header.Add("accept", "*/*")
	req.Header.Add("content-type", "application/x-www-form-urlencoded; charset=UTF-8")
	req.Header.Add("referer", "https://17ce.com/")

	rsp, err := clt.Do(req)
	if err != nil {
		panic(err)
	}
	defer rsp.Body.Close()

	type Ceuser struct {
		Ut   int    `json:"ut"`
		User string `json:"user"`
		Code string `json:"code"`
	}
	ceuser := struct{ Data Ceuser }{}
	dec := json.NewDecoder(rsp.Body)
	if err := dec.Decode(&ceuser); err != nil {
		panic(err)
	}

	wsargs.Add("user", ceuser.Data.User)
	wsargs.Add("code", ceuser.Data.Code)
	wsargs.Add("ut", strconv.Itoa(ceuser.Data.Ut))
}
