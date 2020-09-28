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

//array flags
type arrayFlags []string

func (i *arrayFlags) String() string {
	return strings.Join(*i, ",")
}

func (i *arrayFlags) Set(value string) error {
	*i = append(*i, value)
	return nil
}

//saveDataDocument defines the structure of the save json file
type saveDataDocument struct {
	IP     string `json:"ip"`
	ZoneID string `json:"zoneID"`
}

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

//zoneInfoResponseMessage is the envelope response that includes the zone id
type zoneInfoResponseMessage struct {
	Result []struct {
		ID string `json:"id"`
	} `json:"result"`
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
	cfzone      string
	cfhosts     arrayFlags
	wanIPSource string = "http://icanhazip.com"
	ipRX        *regexp.Regexp
	savePath    string
	verbose     bool
)

func init() {

	flag.StringVar(&cfuser, "cfuser", "", "Cloudflare account username (required)")
	flag.StringVar(&cfkey, "cfkey", "", "Global API Key from My Account > API Keys (required)")
	flag.StringVar(&cfzone, "cfzone", "", "Name of the zone containing the host to update (required)")
	flag.Var(&cfhosts, "cfhost", "Names of the host entries (required)")

	flag.BoolVar(&verbose, "verbose", false, "Enable verbose logging output")
	flag.StringVar(&wanIPSource, "wan-ip-source", wanIPSource, "URL of WAN IP service")

	ipRX = regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}\b`)

	pwd, err := os.Getwd()
	if err != nil {
		log.Fatal(fmt.Errorf("Failed to get working directory: %v", err))
	}
	savePath = path.Join(pwd, "go-cloudflare-ddns-saved.json")

}

func main() {

	flag.Parse()

	//Check mandatory flags
	if cfuser == "" || cfkey == "" || cfzone == "" || len(cfhosts) == 0 {
		flag.Usage()
		os.Exit(1)
		return
	}

	//Get the WAN IP
	ip, err := getWANIP()
	if err != nil {
		log.Fatal(err)
	}
	logVerbose("WAN IP is: %s", ip)

	//Get saved data
	saveData, err := getSaveData()
	if err != nil {
		log.Fatal(err)
	}

	//Verify work is needed
	if strings.Compare(ip, saveData.IP) == 0 {
		log.Print("IP address unchanged - nothing to do.")
		return
	}

	log.Print("New IP address or IP address changed.")
	saveData.IP = ip

	//Get zoneid if not already resolved
	if saveData.ZoneID == "" {
		logVerbose("Getting zoneid for zone: %s", cfzone)
		saveData.ZoneID, err = getZoneID()
		if err != nil {
			log.Fatal(err)
		}
		logVerbose("ZoneID is: %s", saveData.ZoneID)
	}

	for _, cfhost := range cfhosts {

		logVerbose("Updating IP for host: %s", cfhost)

		//Always the hostData for the host record to update, as this also gets the ttl/proxied flag, which are required on the api
		//If we cache this there's a risk of setting it to an old value
		hostData, err := getHostData(saveData.ZoneID, cfhost)
		if err != nil {
			log.Fatal(err)
		}
		logVerbose("HostID is: %s", hostData.ID)

		//Submit to cloudflare
		err = sendIPUpdate(hostData, saveData.ZoneID, cfhost, string(ip))
		if err != nil {
			log.Fatal(err)
		}
	}

	//Persist
	err = setSaveData(saveData)
	if err != nil {
		log.Fatal(err)
	}

	log.Print("IP address update complete.")

}

func logVerbose(format string, a ...interface{}) {
	if !verbose {
		return
	}

	log.Printf(format, a...)
}

func getWANIP() (ip string, err error) {

	ip = ""
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

	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	ip = strings.TrimSpace(string(data))

	if !ipRX.MatchString(ip) {
		err = fmt.Errorf("Response from %v does not look like an IP address: %.25s", wanIPSource, ip)
	}

	return
}

func getSaveData() (saveData saveDataDocument, err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in getSavedData(): %v", err)
		}
	}()

	//check for saved data
	data, readErr := ioutil.ReadFile(savePath)
	if readErr != nil {
		log.Printf("Could not read saved data from file '%v' (this is ok on first run. at other times check file permissions etc)", savePath)
		return
	}

	if err = json.Unmarshal(data, &saveData); err != nil {
		err = fmt.Errorf("Error parsing host details response: %v", err)
		return
	}
	return

}

func setSaveData(saveData saveDataDocument) (err error) {

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in setSaveData(): %v", err)

		}
	}()

	data, err := json.Marshal(saveData)
	if err != nil {
		err = fmt.Errorf("Error preparsing saveData: %v", err)
		return
	}

	//Persist the IP only once upload has succeeded (incase retry is required)
	if err = ioutil.WriteFile(savePath, data, 0644); err != nil {
		log.Fatalf("Failed to save data to file at '%v'", savePath)
	}

	return
}

func getHostData(zoneID string, cfhost string) (hostData hostData, err error) {

	//Example curl request
	// curl -X GET "https://api.cloudflare.com/client/v4/zones/$cfzonekey/dns_records?type=A&name=$cfhost" \
	// 	-H "X-Auth-Key: $cfkey " \
	// 	-H "X-Auth-Email: $cfuser" \
	// 	-H "Content-Type: application/json" > ./cf-ddns.json

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in getHostData(): %v", err)
		}
	}()

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records?type=A&name=%s", zoneID, cfhost)

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

func getZoneID() (zoneID string, err error) {

	//Example curl request
	// curl -X GET "https://api.cloudflare.com/client/v4/zones/?name=$cfhost" \
	// 	-H "X-Auth-Key: $cfkey " \
	// 	-H "X-Auth-Email: $cfuser" \
	// 	-H "Content-Type: application/json" > ./cf-ddns.json

	defer func() {
		if err != nil {
			err = fmt.Errorf("Error in getZoneID(): %v", err)
		}
	}()

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/?name=%s", cfzone)

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
		err = fmt.Errorf("Error requesting zone details %v", url)
		return
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return
	}

	var msg zoneInfoResponseMessage
	if err = json.Unmarshal(body, &msg); err != nil {
		err = fmt.Errorf("Error parsing zone details response: %v", err)
		return
	}
	if len(msg.Result) == 0 || msg.Result[0].ID == "" {
		err = fmt.Errorf("Error reading zone id")
		return
	}
	zoneID = msg.Result[0].ID

	return

}

func sendIPUpdate(hostData hostData, zoneID string, cfhost string, ip string) (err error) {

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

	url := fmt.Sprintf("https://api.cloudflare.com/client/v4/zones/%s/dns_records/%s", zoneID, hostData.ID)

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
	if strings.Compare(ip, msg.Result.Content) != 0 {
		err = errors.New("Error checking that IP was correctly updated")
		return
	}

	return
}
