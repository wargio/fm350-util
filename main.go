package main

import (
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/albenik/go-serial/v2"
	tm "github.com/buger/goterm"
	ag "github.com/guptarohit/asciigraph"
	"github.com/vishvananda/netlink"
	"github.com/warthog618/sms"
	"github.com/warthog618/sms/encoding/pdumode"
	"github.com/warthog618/sms/encoding/tpdu"
)

type NetRegisterStatus int
type NRDLBandwidth int
type NRSC int

type Signal struct {
	SsRsrq float32
	SsRsrp int
	SsRinr float32
}

type Sms struct {
	SrcPhone string
	Message  string
}

const (
	NET_NOT_REGISTERED NetRegisterStatus = iota
	NET_REGISTERED_4G
	NET_REGISTERED_5G
)

var (
	match_crnl = regexp.MustCompile(`[\r\n]+`)
	match_ok   = regexp.MustCompile(`\n+OK`)
	sensors    = []string{
		"soc_max",
		"cpu_little0",
		"cpu_little1",
		"cpu_little2",
		"cpu_little3",
		"gpu0",
		"gpu1",
		"dramc",
		"mmsys",
		"md_5g",
		"md_4g",
		"md_3g",
		"soc_dram_ntc",
		"ltepa_ntc",
		"nrpa_ntc",
		"rf_ntc",
		"md_rf",
		"conn_gps",
		"pmic",
		"pmic_vcore",
		"pmic_vproc",
		"pmic_vgpu",
		"unknown",
	}
	debug bool
)

type Modem struct {
	port *serial.Port
	cid  int
}

func atoi(s string) int {
	d, _ := strconv.Atoi(s)
	return d
}

func atof(s string) float32 {
	d, _ := strconv.Atoi(s)
	return float32(d)
}

func NewModem(device string, baud, timeout, cid int) *Modem {
	if device == "" {
		log.Fatal("missing -serial")
	} else if baud < 1 {
		log.Fatal("invalid -baud value")
	} else if timeout < 1 {
		log.Fatal("invalid -timeout value")
	} else if cid < 1 {
		log.Fatal("invalid -cid value")
	}

	port, err := serial.Open(device,
		serial.WithBaudrate(baud),
		serial.WithDataBits(8),
		serial.WithParity(serial.NoParity),
		serial.WithStopBits(serial.OneStopBit),
		serial.WithReadTimeout(timeout),
		serial.WithWriteTimeout(timeout),
		serial.WithHUPCL(false),
	)
	if err != nil {
		log.Fatal(err)
	}

	m := &Modem{port: port, cid: cid}

	// flush buffer.
	for {
		m.read()
		result := m.send("AT", true)
		if result == "OK" {
			break
		} else if result == "AT\nOK" {
			m.send("ATE0", false)
		}
		time.Sleep(time.Second)
	}
	// set error message format.
	m.send("AT+CMEE=2", false)
	return m
}

func (m *Modem) read() string {
	buf := make([]byte, 2048)
	n, err := m.port.Read(buf)
	if err != nil {
		log.Fatal("Serial: failed to read:", err)
	}

	value := strings.TrimSpace(string(buf[:n]))
	value = match_crnl.ReplaceAllString(value, "\n")
	if debug {
		log.Printf("AT: read: %s\n", value)
	}
	return value
}

func (m *Modem) write(value string) {
	if debug {
		log.Printf("AT: write: %s", value)
	}
	_, err := m.port.Write([]byte(value))
	if err != nil {
		log.Fatal("Serial: failed to write:", err)
	}
}

func (m *Modem) send(command string, keepOk bool) string {
	m.write(command + "\r\n")
	res := m.read()
	if !keepOk {
		if res == "OK" {
			res = ""
		} else {
			res = match_ok.ReplaceAllString(res, "")
		}
	}
	if strings.HasPrefix(res, "ERROR") {
		log.Fatal(res)
	}
	return res
}

func (m *Modem) sendex(command, expect string, keepOk bool) string {
	res := m.send(command, keepOk)
	if strings.HasPrefix(res, expect) {
		return res[len(expect):]
	}
	return res
}

func (m *Modem) Close() {
	m.port.Close()
}

func (m *Modem) ModemReset() {
	log.Printf("%s\n", m.send("AT+CFUN=1,1", true))
}

func (m *Modem) HasSimPIN() bool {
	return m.send("AT+CPIN?", false) == "+CPIN: READY"
}

func (m *Modem) SetSimPIN(pin string) bool {
	if m.send("AT+CPIN?", false) == "+CPIN: READY" {
		return true
	}

	if pin == "" {
		log.Fatal("Modem requires SIM card PIN number.")
		return false
	}

	// set PIN number
	m.send(`AT+CPIN="`+pin+`"`, false)

	res := m.send("AT+CPIN?", false)
	if res == "+CPIN: READY" {
		return true
	}

	log.Fatal(res)
	return false
}

func (m *Modem) IsConnected() bool {
	res := m.send(fmt.Sprintf("AT+CGPADDR=%d", m.cid), false)
	return !strings.Contains(res, "ERROR")
}

func (m *Modem) SetContext(enable bool) {
	state := 0
	if enable {
		state = 1
	}
	command := fmt.Sprintf(`AT+CGACT=%d,%d`, state, m.cid)
	m.send(command, true)
	time.Sleep(time.Second * 5)
}

func (m *Modem) Connect(netdev string, enable bool) {
	net, err := netlink.LinkByName(netdev)
	if err != nil {
		log.Fatal("failed to get netdev "+netdev+":", err)
	}

	if !m.IsConnected() {
		m.SetContext(enable)
		if enable && !m.IsConnected() {
			log.Fatal("Failed to connect.")
		} else if !enable && m.IsConnected() {
			log.Fatal("Failed to disconnect.")
		}
	}
	m.FlushNetDev(net)
	if enable {
		m.SetupNetDev(net)
	}
}

func (m *Modem) SetAPN(apn string) {
	m.send(fmt.Sprintf(`AT+CGDCONT=%d,"IPV4V6","%s"`, m.cid, apn), false)
}

func (m *Modem) GetNetRegisterStatus() NetRegisterStatus {
	if !strings.HasPrefix(m.send("AT+C5GREG?", false), "0,") {
		return NET_REGISTERED_5G
	}
	if !strings.HasPrefix(m.send("AT+CEREG?", false), "0,") {
		return NET_REGISTERED_4G
	}
	return NET_NOT_REGISTERED
}

func (m *Modem) NetworkOperator() string {
	res := m.send("AT+GTCURCAR?", false)
	if !strings.HasPrefix(res, "+GTCURCAR: ") {
		return "Unknown"
	}

	info := strings.Split(res[11:], ",")
	if len(info) > 1 {
		return strings.Trim(info[1], `"`)
	}
	return ""
}

func (m *Modem) SignalInfo() Signal {
	signal := Signal{}

	res := m.send(`AT+CESQ`, false)
	if !strings.HasPrefix(res, "+CESQ: ") {
		return signal
	}

	info := strings.Split(res[7:], ",")
	if len(info) != 9 {
		return signal
	}

	// synchronization signal based reference signal received quality
	signal.SsRsrq = atof(info[6])
	if signal.SsRsrq < 128 {
		signal.SsRsrq *= 0.5
		signal.SsRsrq += -43 /*dB*/
	} else {
		signal.SsRsrq = 0
	}

	// synchronization signal based reference signal received power
	signal.SsRsrp = atoi(info[7])
	if signal.SsRsrp < 128 {
		signal.SsRsrp += -156 /* dBm */
	} else {
		signal.SsRsrp = 0
	}

	// synchronization signal based signal to noise and interference ratio
	signal.SsRinr = atof(info[8])
	if signal.SsRinr < 128 {
		signal.SsRinr *= 0.5
		signal.SsRinr += -23 /*dB*/
	} else {
		signal.SsRinr = 0
	}

	return signal
}

func asIpAddr(badip string, masked bool) string {
	badip = strings.Trim(badip, `"`)
	tokens := strings.Split(badip, ".")
	if len(tokens) == 4 {
		if masked {
			return badip + "/24"
		}
		return badip
	}
	ipv6 := ""
	for i, token := range tokens {
		if i > 0 && (i&1) == 0 {
			ipv6 += ":"
		}
		ipv6 += fmt.Sprintf("%02x", atoi(token))
	}
	if masked {
		return ipv6 + "/64"
	}
	return ipv6
}

func (m *Modem) FlushNetDev(link netlink.Link) {
	netlink.LinkSetDown(link)
	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		log.Fatal("failed flush netdev:", err)
	}
	for _, addr := range addrs {
		if len(addr.IP) != 4 {
			continue
		}
		// remove old config.
		netlink.AddrDel(link, &addr)
	}
}

func (m *Modem) SetupNetDev(link netlink.Link) {
	netdev := link.Attrs().Name
	res := m.send(fmt.Sprintf("AT+CGPADDR=%d", m.cid), false)
	if !strings.HasPrefix(res, "+CGPADDR: ") {
		log.Fatal("Failed to get local net configuration.")
	}
	log.Println("Connecting the modem to the network via", netdev, "device.")

	tokens := strings.Split(res[10:], ",")
	for i, tok := range tokens {
		if i < 1 {
			continue
		}
		ip := asIpAddr(tok, true)
		addr, err := netlink.ParseAddr(ip)
		if err != nil {
			log.Println("failed to parse "+ip, err)
			continue
		}
		if addr.IP.To4() == nil {
			// ignore IPv6 since DHCPv6 is enough.
			continue
		}
		err = netlink.AddrAdd(link, addr)
		if err != nil {
			log.Println("failed to add address", addr, "to", netdev, ": ", err)
			continue
		}
		log.Println(netdev, "now has ip", ip)
	}
	netlink.LinkSetUp(link)
}

func (m *Modem) SetRoute4(netdev string) {
	link, err := netlink.LinkByName(netdev)
	if err != nil {
		log.Fatal("Failed to get netdev "+netdev+": ", err)
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		log.Fatal("Failed get netdev addresses: ", err)
	}
	for _, addr := range addrs {
		if len(addr.IP) != 4 {
			continue
		}

		gw := addr.IP[:]
		gw[3] = byte(1)
		route := netlink.Route{LinkIndex: link.Attrs().Index, Dst: nil, Gw: gw}
		err = netlink.RouteAdd(&route)
		if err != nil {
			log.Fatal("Failed to add route:", err)
		}
		log.Println("Added route to gw: ", gw)
	}
}

func (m *Modem) DNS() []string {
	dns := []string{}
	res := m.send(fmt.Sprintf("AT+GTDNS=%d", m.cid), false)
	if !strings.HasPrefix(res, "+GTDNS: ") {
		return dns
	}
	lines := strings.Split(res, "\n")
	for _, line := range lines {
		info := strings.Split(line[12:], ",")
		for i, ip := range info {
			if i < 1 {
				continue
			}
			dns = append(dns, asIpAddr(ip, false))
		}
	}
	return dns
}

func (m *Modem) ModemInfo(asJson bool) {
	firmware := strings.Trim(m.sendex("AT+GTAPPVER?", "+GTAPPVER: ", false), `"`)
	serialnum := strings.Trim(m.sendex("AT+CFSN", "+CFSN: ", false), `"`)
	version := strings.Trim(m.sendex("AT+GTPKGVER?", "+GTPKGVER: ", false), `"`)
	imei := m.send("AT+CGSN", false)
	hasSim := m.HasSimPIN()
	netstat := m.GetNetRegisterStatus()
	operator := m.NetworkOperator()
	signal := m.SignalInfo()

	if asJson {
		obj := map[string]interface{}{
			"firmware":     firmware,
			"serialnumber": serialnum,
			"version":      version,
			"imei":         imei,
			"has_sim":      hasSim,
			"net_status":   netstat,
			"signal":       signal,
		}
		s, _ := json.Marshal(obj)
		fmt.Println(string(s))
	} else {
		log.Printf("Firmware:   %s\n", firmware)
		log.Printf("Serial Num: %s\n", serialnum)
		log.Printf("Version:    %s\n", version)
		log.Printf("IMEI:       %s\n", imei)
		log.Printf("Has SIM:    %v\n", hasSim)
		switch netstat {
		case NET_NOT_REGISTERED:
			log.Printf("Net Type:   Not connected\n")
		case NET_REGISTERED_4G:
			log.Printf("Net Type:   4G\n")
		case NET_REGISTERED_5G:
			log.Printf("Net Type:   5G\n")
		}
		log.Printf("Operator:   %s\n", operator)
		log.Printf("Signal:     %d dBm (q:%.1f, n:%.1f dB)\n", signal.SsRsrp, signal.SsRsrq, signal.SsRinr)
	}
}

func (m *Modem) SetBands() {
	m.sendex(`AT+EPBSEH="FF","FFFF","ffffffff","ffffffffffffffff"`, "+CIREPI", false)
	time.Sleep(time.Second * 5)
}

func ratAsString(rat string) string {
	irat := atoi(rat)
	switch irat {
	case 1:
		return "UMTS (1)"
	case 2:
		return "LTE (2)"
	case 4:
		return "LTE/UMTS (4)"
	case 10:
		return "Automatic (10)"
	case 14:
		return "NR-RAN (14)"
	case 16:
		return "NR-RAN/WCDMA (16)"
	case 17:
		return "NR-RAN/LTE (17)"
	case 20:
		return "NR-RAN/WCDMA/LTE (20)"
	default:
		return fmt.Sprintf("Unknown (%d)", irat)
	}
}

func preferredActAsString(preferred string) string {
	iPrefAct := atoi(preferred)
	switch iPrefAct {
	case 2:
		return "WCDMA (2)"
	case 3:
		return "LTE (3)"
	case 6:
		return "NR-RAN (6)"
	default:
		return fmt.Sprintf("Unknown (%d)", iPrefAct)
	}
}

func bandAsString(sband string) string {
	band := atoi(sband)
	if band >= 1 && band <= 10 {
		return fmt.Sprintf("UMTS_%d", band)
	} else if band >= 101 && band <= 171 {
		return fmt.Sprintf("LTE_%d", band-100)
	} else if band >= 501 && band <= 509 {
		return fmt.Sprintf("NR_%d", band-500)
	} else if band >= 5010 && band <= 5099 {
		return fmt.Sprintf("NR_%d", band-5000)
	} else if band >= 50100 && band <= 50512 {
		return fmt.Sprintf("NR_%d", band-50000)
	}
	return fmt.Sprintf("%d", band)
}

func (m *Modem) ModemBands(asJson bool) {
	obj := map[string]interface{}{}
	bnds := []string{}
	bands := strings.Split(m.sendex(`AT+GTACT?`, "+GTACT: ", false), `,`)
	rat := ratAsString(bands[0])
	pref1 := preferredActAsString(bands[1])
	pref2 := preferredActAsString(bands[2])
	if !asJson {
		log.Printf("RAT: %s\n", rat)
		log.Printf("Preferred Act 1: %s\n", pref1)
		log.Printf("Preferred Act 2: %s\n", pref2)
	}

	for i, band := range bands {
		if i < 3 {
			continue
		}
		bnds = append(bnds, bandAsString(band))
	}

	if asJson {
		obj["rat"] = rat
		obj["preferred_act1"] = pref1
		obj["preferred_act2"] = pref2
		obj["bands"] = bnds
		s, _ := json.Marshal(obj)
		fmt.Println(string(s))
	} else {
		log.Printf("Bands: %v\n", bnds)
	}
}

func (m *Modem) ModemTemperature(asJson bool) {
	obj := map[string]interface{}{}
	lines := strings.Split(m.send("AT+GTSENRDTEMP=0", false), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "+GTSENRDTEMP: ") {
			if !asJson {
				log.Println("Bad response:", line)
			}
			continue
		}
		toks := strings.Split(line[14:], ",")
		index := atoi(toks[0]) - 1
		if index < 0 || index >= len(sensors) {
			if !asJson {
				log.Println("Bad index", index, line)
			}
			continue
		}
		temp := atof(toks[1]) / 1000.
		sensor := sensors[index]
		if asJson {
			obj[sensor] = temp
		} else {
			log.Printf("%-13s %.3f\n", sensor+":", temp)
		}
	}

	if asJson {
		s, _ := json.Marshal(obj)
		fmt.Println(string(s))
	}
}

func decodeSms(hexstr string) *Sms {
	data, err := hex.DecodeString(hexstr)
	if err != nil {
		log.Printf("Error decoding hex string: %v\n", err)
		return nil
	}

	pdum, err := pdumode.UnmarshalBinary(data)
	if err != nil {
		log.Printf("Error decoding pdumode: %v\n", err)
		return nil
	}

	pdu, err := sms.Unmarshal(pdum.TPDU)
	if err != nil {
		pdu, err = sms.Unmarshal(data)
		if err != nil {
			log.Printf("Error unmarshal pdu: %v\n", err)
			return nil
		}
	}

	msg, err := sms.Decode([]*tpdu.TPDU{pdu})
	if err != nil {
		log.Printf("Error decoding message: %v\n", err)
		return nil
	}

	return &Sms{
		SrcPhone: pdu.OA.Number(),
		Message:  string(msg),
	}
}

func (m *Modem) SmsList(asJson bool) {
	res := m.send(`AT+CMGL=4`, false)
	if !strings.HasPrefix(res, "+CMGL: ") {
		return
	}
	lines := strings.Split(res, "\n")
	var messages []*Sms
	for _, line := range lines {
		if strings.HasPrefix(line, "+CMGL:") {
			continue
		}
		sms := decodeSms(line)
		if asJson {
			messages = append(messages, sms)
		} else {
			fmt.Println(sms.SrcPhone, sms.Message)
		}
	}

	if asJson {
		s, _ := json.Marshal(messages)
		fmt.Println(string(s))
	}
}

func (m *Modem) plotSignal() {
	if !m.IsConnected() {
		log.Fatal("Modem is not connected!")
	}
	const datasize = 32
	data := []float64{}
	for {
		tm.Clear()
		signal := m.SignalInfo()
		if signal.SsRsrp == 0 {
			time.Sleep(time.Second)
			continue
		}
		strength := float64(signal.SsRsrp)
		if len(data) < datasize {
			data = append(data, strength)
		} else {
			for i := 0; i < (datasize - 1); i++ {
				data[i] = data[i+1]
			}
			data[datasize-1] = strength
		}

		now := time.Now()
		tm.MoveCursor(1, 1)
		tm.Printf("Current Time: %s | p:%d dBm | q:%.1f dB | n:%.1f dB\n", now.Format(time.RFC1123), signal.SsRsrp, signal.SsRsrq, signal.SsRinr)

		graph := ag.Plot(data,
			ag.Width(80),
			ag.Height(20),
			ag.UpperBound(-28.),
			ag.LowerBound(-158.),
		)
		tm.Println(graph)
		tm.Flush()
		time.Sleep(time.Second)
	}
}

func findNetDev() string {
	const rndis_path = "/sys/bus/usb/drivers/rndis_host/"
	entries, err := os.ReadDir(rndis_path)
	if err != nil {
		log.Fatal(err)
	}

	for _, e := range entries {
		if !strings.Contains(e.Name(), ":") {
			continue
		}
		subpath := filepath.Join(rndis_path, e.Name(), "net")
		entries2, err := os.ReadDir(subpath)
		if err != nil {
			continue
		}
		for _, e2 := range entries2 {
			return e2.Name()
		}
	}

	log.Fatal("failed to find netdev. please define it using `-netdev ethX`")
	return ""
}

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var restart, graph, smslist, connect, disconnect, info, dns, route, temp, bands, asJson bool
	var baud, timeout, cid int
	var serial, netdev, simpin, apn string
	flag.IntVar(&baud, "baud", 115200, "Serial device baud rate")
	flag.IntVar(&timeout, "timeout", 300, "Serial device timeout in milliseconds")
	flag.IntVar(&cid, "cid", 5, "PDP context id.")
	flag.StringVar(&serial, "serial", "", "Serial device to use (usually /dev/ttyUSB2 or /dev/ttyUSB4)")
	flag.StringVar(&netdev, "netdev", findNetDev(), "Manually sets the net device to use")
	flag.StringVar(&simpin, "simpin", "", "Sets the SIM card PIN number")
	flag.StringVar(&apn, "apn", "", "Sets the APN")
	flag.BoolVar(&graph, "graph", false, "Shows signal graph")
	flag.BoolVar(&restart, "restart", false, "Restarts the modem")
	flag.BoolVar(&smslist, "sms", false, "Prints all the sms received")
	flag.BoolVar(&connect, "connect", false, "Connects and sets up the modem")
	flag.BoolVar(&route, "route", false, "Add default route via modem")
	flag.BoolVar(&disconnect, "disconnect", false, "Disconnects the modem")
	flag.BoolVar(&dns, "dns", false, "Prints the DNS configuration from the ISP.")
	flag.BoolVar(&info, "info", false, "Prints the modem info")
	flag.BoolVar(&temp, "temp", false, "Prints the modem temperature info")
	flag.BoolVar(&bands, "bands", false, "Prints the current modem bands")
	flag.BoolVar(&asJson, "json", false, "Outputs in json format (info, temp, bands, dns, sms only).")
	flag.BoolVar(&debug, "debug", false, "Prints all the AT commands")

	flag.Parse()

	if !restart && !graph && !smslist && !connect && !disconnect && !route && !dns && !info && !temp && !bands {
		log.Fatal("requires a mode: graph | restart | sms | connect | disconnect | route | dns | info | temp")
		return
	}

	modem := NewModem(serial, baud, timeout, cid)
	defer modem.Close()

	if restart {
		modem.ModemReset()
		return
	}

	if graph {
		modem.plotSignal()
		return
	}

	if smslist {
		modem.SmsList(asJson)
		return
	}

	if info {
		modem.ModemInfo(asJson)
		return
	}

	if temp {
		modem.ModemTemperature(asJson)
		return
	}

	if bands {
		modem.ModemBands(asJson)
		return
	}

	if dns {
		if !modem.IsConnected() {
			log.Fatal("Modem needs to be connected to retrieve the DNS configuration.")
			return
		}
		dns := modem.DNS()
		if asJson {
			s, _ := json.Marshal(dns)
			fmt.Println(string(s))
		} else {
			for _, ip := range dns {
				fmt.Println(ip)
			}
		}
		return
	}

	if connect {
		if !modem.IsConnected() {
			if !modem.HasSimPIN() {
				if simpin == "" {
					log.Fatal("Modem requires SIM card PIN number.")
				}
				modem.SetSimPIN(simpin)
			}
			if apn == "" {
				log.Fatal("Cannot connect without an apn.")
				return
			}
			modem.SetAPN(apn)
		}
		modem.Connect(netdev, true)
	}

	if disconnect {
		if !modem.IsConnected() {
			log.Fatal("Modem already disconnected.")
			return
		}
		modem.Connect(netdev, false)
	}

	if !disconnect && route {
		modem.SetRoute4(netdev)
	}
}
