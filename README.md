# Terminal tool for the FM350-XX Modules

Tested with FM350-GL

## cli usage

```
$ ./fm350-util -h
Usage of ./fm350-util:
  -apn string
        Sets the APN
  -baud int
        Serial device baud rate (default 115200)
  -cid int
        PDP context id. (default 5)
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
        Outputs in json format (info, dns, sms only).
  -netdev string
        Manually sets the net device to use (default "eth4")
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
  -timeout int
        Serial device timeout in milliseconds (default 300)
```

## how to build

```bash
CGO_ENABLED=0 go build -v
```
