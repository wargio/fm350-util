# Terminal tool for the FM350-XX Modules

Tested with FM350-GL

## cli usage

```
Usage of fm350-util:
  -apn string
        Sets the APN
  -bands
        Prints the current modem bands
  -baud int
        Serial device baud rate (default 115200)
  -cid int
        PDP context id. (default 5)
  -config string
        path to yaml configuration (default "/etc/fm350/fm350.yaml")
  -connect
        Connects and sets up the modem
  -debug
        Prints all the AT commands
  -disconnect
        Disconnects the modem
  -dns
        Prints the DNS configuration from the ISP.
  -graph
        Shows signal graph
  -info
        Prints the modem info
  -json
        Outputs in json format (info, temp, bands, dns, sms only).
  -netdev string
        Manually sets the net device to use (default "eth2")
  -ntp string
        Updates the time using the defined ntp server
  -restart
        Restarts the modem
  -route
        Add default route via modem
  -serial string
        Serial device to use (usually /dev/ttyUSB2 or /dev/ttyUSB4)
  -simpin string
        Sets the SIM card PIN number
  -sms
        Prints all the sms received
  -temp
        Prints the modem temperature info
  -timeout int
        Serial device timeout in milliseconds (default 300)
```

## Example usage

### Via command line

```bash
# connect
./fm350-util -connect -serial /dev/ttyUSB4 -simpin 123546 -apn ispapn -route
# disconnect
./fm350-util -disconnect -serial /dev/ttyUSB4
```

### Via saved configuration

Save this at `/etc/fm350/fm350.yaml`

```yaml
serial: "/dev/ttyUSB4"
simpin: "123456"
apn: "ispapn"
route: true
ntp: "whatever.ntp.com"
```

Then run the util

```bash
# connect
./fm350-util -connect
# connect with optional custom path
./fm350-util -connect -config /path/to/config.yaml
# disconnect
./fm350-util -disconnect
# disconnect with optional custom path
./fm350-util -disconnect -config /path/to/config.yaml
```

## how to build

```bash
CGO_ENABLED=0 go build -v
```

for other archs:

```
CGO_ENABLED=0 GOARCH=arm64 go build -v
```
