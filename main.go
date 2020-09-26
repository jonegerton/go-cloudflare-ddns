package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"time"
)

//hostData is the excerpt of a larger response to return the ID only.
//plus a couple of things that have to be echoed back when PUTting updates
type hostData struct {
	ID      string `json:"id"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

//hostResponseMessage is the envelope response that includes the hostData
type hostInfoResponseMessage struct {
	Result []hostData `json:"result"`
}

// updateRequestBody is the submission body to
// data="{\"type\":\"A\",\"name\":\"$cfhost\",\"content\":\"$WAN_IP\",\"ttl\":$cfttl,\"proxied\":$cfproxied}"
type updateRequestBody struct {
	Type    string `json:"type"`
	Name    string `json:"name"`
	Content string `json:"content"`
	TTL     int    `json:"ttl"`
	Proxied bool   `json:"proxied"`
}

// updateResponseMessage
type updateResponseMessage struct {
	Result struct {
		Content string `json:"content"`
	} `json:"result"`
}

var (
	cfuser      string
	cfkey       string
	cfzonekey   string
	cfhost      string
	wanIPSource string = "http://icanhazip.com"
	ipRX        *regexp.Regexp
	savePath    string
)

func init() {

	flag.StringVar(&cfuser, "cfuser", "", "Cloudflare account username")
	flag.StringVar(&cfkey, "cfkey", "", "Global API Key from My Account > API Keys")
	flag.StringVar(&cfzonekey, "cfzonekey", "", "Zone ID from zone overview page")
	flag.StringVar(&cfhost, "cfhost", "", "Name of the host entry")

	flag.StringVar(&wanIPSource, "wan-ip-source", wanIPSource, "URL of WAN IP service")

	ipRX = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal(fmt.Errorf("Failed to get working directory: %v", err))
	}
	savePath = path.Join(pwd, "go-cloudflare-ddns-saved-ip.txt")

}

func main() {

	flag.Parse()

	//Get the WAN IP
	ip, err := getWANIP()
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("WAN IP is: %s", string(ip))

	//check for saved IP
	savedIP, err := ioutil.ReadFile(savePath)
	if err != nil {
		log.Printf("Could not read saved ip from file '%v' (this is ok on first run. at other times check file permissions etc)", savePath)
	}

	//Verify work is needed
	if bytes.Compare(ip, savedIP) == 0 {
		log.Printf("IP address unchanged - nothing to do.")
		return
	}
	log.Print("IP address changed - updating.")

	//Get the ID for the host record to update
	hostData, err := getHostData()
	if err != nil {
		log.Fatal(err)
	}

	//Submit to cloudflare
	err = sendIPUpdate(hostData, string(ip))
	if err != nil {
		log.Fatal(err)
	}

	//Persist the IP only once upload has succeeded (incase retry is required)
	if err = ioutil.WriteFile(savePath, ip, 0644); err != nil {
		log.Fatalf("Failed to save ip address to file at '%v'", savePath)
	}

	log.Print("IP address update complete.")

}

func getWANIP() (ip []byte, err error) {

	ip = nil
	err = nil

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in getWANIP(): %v", err)
		}
	}()

	//Requires service that returns the IP as the entire response body, eg:
	//http://ipinfo.io/ip
	//http://icanhazip.com
	//http://checkip.amazonaws.com/

	req, _ := http.NewRequest("GET", wanIPSource, nil)

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	if resp == nil {
		err = fmt.Errorf("Error requesting WAN IP from %v", wanIPSource)
		return
	}
	defer resp.Body.Close()

	ip, err = ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	if !ipRX.Match(ip) {
		err = fmt.Errorf("Response from %v does not look like an IP address: %.25s", wanIPSource, ip)
	}

	return

}

func getHostData() (hostData hostData, err error) {

	//Example curl request
	// curl -X GET "https://api.cloudflare.com/client/v4/zones/$cfzonekey/dns_records?type=A&name=$cfhost" \
	// 	-H "X-Auth-Key: $cfkey " \
	// 	-H "X-Auth-Email: $cfuser" \
	// 	-H "Content-Type: application/json" > ./cf-ddns.json

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in getHostID(): %v", err)
		}
	}()

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=A&name=%s", cfzonekey, cfhost)

	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("X-Auth-Key", cfkey)
	req.Header.Set("X-Auth-Email", cfuser)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	if resp == nil {
		err = fmt.Errorf("Error requesting host details %v", url)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var msg hostInfoResponseMessage
	if err = json.Unmarshal(body, &msg); err != nil {
		err = fmt.Errorf("Error parsing host details response: %v", err)
		return
	}
	if len(msg.Result) == 0 || msg.Result[0].ID == "" {
		err = fmt.Errorf("Error reading host id")
		return
	}
	hostData = msg.Result[0]

	return

}

func sendIPUpdate(hostData hostData, ip string) (err error) {

	//Curl example
	// data="{\"type\":\"A\",\"name\":\"$cfhost\",\"content\":\"$WAN_IP\",\"ttl\":$cfttl,\"proxied\":$cfproxied}"
	// echo "data: $data" >> $log

	// curl -X PUT "https://api.cloudflare.com/client/v4/zones/$cfzonekey/dns_records/$cfhostkey" \
	// 	-H "X-Auth-Key: $cfkey" \
	// 	-H "X-Auth-Email: $cfuser" \
	// 	-H "Content-Type: application/json" \
	// 	--data $data >> $log

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in sendIPUpdate(): %v", err)
		}
	}()

	data := updateRequestBody{
		Type:    "A",
		Name:    cfhost,
		Content: ip,
		TTL:     hostData.TTL,
		Proxied: hostData.Proxied,
	}
	putBody, err := json.Marshal(data)
	if err != nil {
		err = fmt.Errorf("Error in sendIPUpdate(): %v", err)
	}

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", cfzonekey, hostData.ID)

	req, _ := http.NewRequest("PUT", url, bytes.NewBuffer(putBody))
	req.Header.Set("X-Auth-Key", cfkey)
	req.Header.Set("X-Auth-Email", cfuser)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: time.Second * 10,
	}

	resp, err := client.Do(req)
	if err != nil {
		return
	}
	if resp == nil {
		err = fmt.Errorf("Error sending host update details %v", url)
		return
	}
	defer resp.Body.Close()

	resBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var msg updateResponseMessage
	if err = json.Unmarshal(resBody, &msg); err != nil {
		err = fmt.Errorf("Error parsing host details response: %v", err)
		return
	}
	if msg.Result.Content == "" {
		err = fmt.Errorf("Error reading updated IP")
		return
	}

	//Check IP on response matches submit
	if strings.Compare(ip, msg.Result.Content) == 0 {
		err = errors.New("Error checking that IP was correctly updated")
		return
	}

	return
}
