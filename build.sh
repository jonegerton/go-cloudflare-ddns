#Build for windows

#Build for linux x64
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build
tar -czf go-cloudflare-ddns-linux-amd64.tar.gz go-cloudflare-ddns
rm go-cloudflare-ddns

#Build for linux arm
CGO_ENABLED=0 GOOS=linux GOARCH=arm go build
tar -czf go-cloudflare-ddns-linux-arm.tar.gz go-cloudflare-ddns
rm go-cloudflare-ddns

#Build for windows x64
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build
zip go-cloudflare-ddns-win.zip go-cloudflare-ddns.exe
rm go-cloudflare-ddns.exe
