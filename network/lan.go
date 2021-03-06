package network

import (
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
)

const LANDefaultttl = 10
const LANAliasesFile = "~/bettercap.aliases"

type EndpointNewCallback func(e *Endpoint)
type EndpointLostCallback func(e *Endpoint)

type LAN struct {
	sync.Mutex

	hosts           map[string]*Endpoint
	iface           *Endpoint
	gateway         *Endpoint
	ttl             map[string]uint
	aliases         *Aliases
	newCb           EndpointNewCallback
	lostCb          EndpointLostCallback
	aliasesFileName string
}

type lanJSON struct {
	Hosts []*Endpoint `json:"hosts"`
}

func NewLAN(iface, gateway *Endpoint, newcb EndpointNewCallback, lostcb EndpointLostCallback) *LAN {
	err, aliases := LoadAliases()
	if err != nil {
		fmt.Printf("%s\n", err)
	}

	return &LAN{
		iface:   iface,
		gateway: gateway,
		hosts:   make(map[string]*Endpoint),
		ttl:     make(map[string]uint),
		aliases: aliases,
		newCb:   newcb,
		lostCb:  lostcb,
	}
}

func (l *LAN) MarshalJSON() ([]byte, error) {
	doc := lanJSON{
		Hosts: make([]*Endpoint, 0),
	}

	for _, h := range l.hosts {
		doc.Hosts = append(doc.Hosts, h)
	}

	return json.Marshal(doc)
}

func (lan *LAN) SetAliasFor(mac, alias string) bool {
	lan.Lock()
	defer lan.Unlock()

	mac = NormalizeMac(mac)
	if e, found := lan.hosts[mac]; found {
		lan.aliases.Set(mac, alias)
		e.Alias = alias
		return true
	}
	return false
}

func (lan *LAN) Get(mac string) (*Endpoint, bool) {
	lan.Lock()
	defer lan.Unlock()

	if e, found := lan.hosts[mac]; found == true {
		return e, true
	}
	return nil, false
}

func (lan *LAN) List() (list []*Endpoint) {
	lan.Lock()
	defer lan.Unlock()

	list = make([]*Endpoint, 0)
	for _, t := range lan.hosts {
		list = append(list, t)
	}
	return
}

func (lan *LAN) WasMissed(mac string) bool {
	if mac == lan.iface.HwAddress || mac == lan.gateway.HwAddress {
		return false
	}

	lan.Lock()
	defer lan.Unlock()

	if ttl, found := lan.ttl[mac]; found == true {
		return ttl < LANDefaultttl
	}
	return true
}

func (lan *LAN) Remove(ip, mac string) {
	lan.Lock()
	defer lan.Unlock()

	if e, found := lan.hosts[mac]; found {
		lan.ttl[mac]--
		if lan.ttl[mac] == 0 {
			delete(lan.hosts, mac)
			delete(lan.ttl, mac)
			lan.lostCb(e)
		}
		return
	}
}

func (lan *LAN) shouldIgnore(ip, mac string) bool {
	// skip our own address
	if ip == lan.iface.IpAddress {
		return true
	}
	// skip the gateway
	if ip == lan.gateway.IpAddress {
		return true
	}
	// skip broadcast addresses
	if strings.HasSuffix(ip, BroadcastSuffix) {
		return true
	}
	// skip broadcast macs
	if strings.ToLower(mac) == BroadcastMac {
		return true
	}
	// skip everything which is not in our subnet (multicast noise)
	addr := net.ParseIP(ip)
	return lan.iface.Net.Contains(addr) == false
}

func (lan *LAN) Has(ip string) bool {
	lan.Lock()
	defer lan.Unlock()

	for _, e := range lan.hosts {
		if e.IpAddress == ip {
			return true
		}
	}

	return false
}

func (lan *LAN) EachHost(cb func(mac string, e *Endpoint)) {
	lan.Lock()
	defer lan.Unlock()

	for m, h := range lan.hosts {
		cb(m, h)
	}
}

func (lan *LAN) GetByIp(ip string) *Endpoint {
	lan.Lock()
	defer lan.Unlock()

	for _, e := range lan.hosts {
		if e.IpAddress == ip {
			return e
		}
	}

	return nil
}

func (lan *LAN) AddIfNew(ip, mac string) *Endpoint {
	lan.Lock()
	defer lan.Unlock()

	mac = NormalizeMac(mac)

	if lan.shouldIgnore(ip, mac) {
		return nil
	} else if t, found := lan.hosts[mac]; found {
		if lan.ttl[mac] < LANDefaultttl {
			lan.ttl[mac]++
		}
		return t
	}

	e := NewEndpointWithAlias(ip, mac, lan.aliases.Get(mac))

	lan.hosts[mac] = e
	lan.ttl[mac] = LANDefaultttl

	lan.newCb(e)

	return nil
}
