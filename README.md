# go-cloudflare-ddns

cloudflare ddns client in go

This is a golang implementation of my [sh script version](https://github.com/jonegerton/cloudflare-ddns)

This utility is working as at 28-Sep-2020. Changes to the api by CloudFlare will likely break them.

## Flags

- cfuser: Cloudflare account username (required)
- cfkey: Global API Key from My Account > API Keys (required)
- cfzone: Name of the zone containing the host to update (required)
- cfhost: Names of the host entries (required). Multiple values are supported.
- verbose: Enable verbose logging output
- wan-ip-source: URL of WAN IP service

## Usage

The utility is most easily used by creating s script to run it, containing the parameters needed. This can then be run using a scheduler to keep cloudflare updated.

The utility supports updated multiple hosts on the same zone by setting the -cfhost flag multiple times in the command. If you need to update multiple zones, then create two copies of the utility in separate folders, one for each zone.

The utility saves the current IP address and the id for the zone in a json file (go-cloudflare-ddns-saved.json) in the same folder as the utility. To force an ip update delete this file.

### Linux .sh script

    cfkey=<key>
    cfuser=<user email address>
    cfzone=<zone name>
    cfhost=<host to be updated>

    ./go-cloudflare-ddns -cfuser=$cfuser -cfkey=$cfkey -cfhost=$cfhost -cfzone=$cfzone

### Windows .bat file

    SET cfkey=<key>
    SET cfuser=<user email address>
    SET cfzone=<zone name>
    SET cfhost=<host to be updated>

    go-cloudflare-ddns.exe -cfuser=%cfuser% -cfkey=%cfkey% -cfhost=%cfhost% -cfzone=%cfzone%

## IP source

By default the utility uses the site http://icanhazip.com to use the IP address, but this can be overriden.

Use the `wan-ip-source` flag to specify a different source.

Sites used must return only the IP address in the response body.

Example suitable sites include:

- http://ipinfo.io/ip
- http://icanhazip.com
- http://checkip.amazonaws.com/