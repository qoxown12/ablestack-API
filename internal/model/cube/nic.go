package cube

import (
	"sync"
	"time"
)

// NICDevice는 NIC 목록 응답 항목입니다.
type NICDevice struct {
	Device  string `json:"DEVICE"`
	Type    string `json:"TYPE"`
	State   string `json:"STATE"`
	ConPath string `json:"CON-PATH,omitempty"`
	Driver  string `json:"DRIVER,omitempty"`
	PCI     string `json:"PCI,omitempty"`
	Speed   string `json:"SPEED,omitempty"`
	Model   string `json:"MODEL,omitempty"`
	MTU     *int   `json:"MTU,omitempty"`
	MAC     string `json:"MAC,omitempty"`
	// 상세 정보용
	IPv4    *NICIPInfo  `json:"IPV4,omitempty"`
	IPv6    *NICIPInfo  `json:"IPV6,omitempty"`
	Members []string    `json:"MEMBERS,omitempty"`
	Bond    *BondDetail `json:"BOND,omitempty"`
}

// NICAddress는 IP 설정 정보를 담습니다.
type NICAddress struct {
	Family    string `json:"FAMILY"`
	Address   string `json:"ADDRESS"`
	Prefixlen int    `json:"PREFIXLEN"`
	Scope     string `json:"SCOPE,omitempty"`
}

// NICIPInfo는 IPv4/IPv6 활성 여부와 주소 목록을 담습니다.
type NICIPInfo struct {
	Enable    *bool        `json:"ENABLE,omitempty"`
	Addresses []NICAddress `json:"ADDRESSES,omitempty"`
}

// BondDetail은 bond 상세 정보를 담습니다.
type BondDetail struct {
	Mode            string `json:"MODE,omitempty"`
	ActiveSlave     string `json:"ACTIVE_SLAVE,omitempty"`
	Primary         string `json:"PRIMARY,omitempty"`
	PrimaryReselect string `json:"PRIMARY_RESELECT,omitempty"`
	XmitHashPolicy  string `json:"XMIT_HASH_POLICY,omitempty"`
	LACPActive      string `json:"LACP_ACTIVE,omitempty"`
	LACPRate        string `json:"LACP_RATE,omitempty"`
	AdSelect        string `json:"AD_SELECT,omitempty"`
	FailOverMac     string `json:"FAIL_OVER_MAC,omitempty"`
	Miimon          *int   `json:"MIIMON,omitempty"`
	UpDelay         *int   `json:"UPDELAY,omitempty"`
	DownDelay       *int   `json:"DOWNDELAY,omitempty"`
}

// TypeNICStatus는 내부 캐시/처리용 구조입니다.
type TypeNICStatus struct {
	Bridges     []NICDevice `json:"bridges,omitempty"`
	Ethernets   []NICDevice `json:"ethernets,omitempty"`
	Bonds       []NICDevice `json:"bonds,omitempty"`
	Others      []NICDevice `json:"others,omitempty"`
	RefreshTime time.Time   `json:"-"`
	mu          *sync.RWMutex
} // @name TypeNICStatus

// NICResponse는 API 응답 DTO입니다.
type NICResponse struct {
	Bridges     []NICDevice `json:"bridges,omitempty"`
	Ethernets   []NICDevice `json:"ethernets,omitempty"`
	Bonds       []NICDevice `json:"bonds,omitempty"`
	Others      []NICDevice `json:"others,omitempty"`
	RefreshTime string      `json:"refresh_time,omitempty"`
} // @name NICResponse

var lockNICStatus sync.Once
var _NICStatus *TypeNICStatus

func NIC() *TypeNICStatus {
	lockNICStatus.Do(func() {
		_NICStatus = &TypeNICStatus{mu: &sync.RWMutex{}}
	})
	return _NICStatus
}

func (n *TypeNICStatus) Lock() {
	if n == nil || n.mu == nil {
		return
	}
	n.mu.Lock()
}

func (n *TypeNICStatus) Unlock() {
	if n == nil || n.mu == nil {
		return
	}
	n.mu.Unlock()
}

func (n *TypeNICStatus) RLock() {
	if n == nil || n.mu == nil {
		return
	}
	n.mu.RLock()
}

func (n *TypeNICStatus) RUnlock() {
	if n == nil || n.mu == nil {
		return
	}
	n.mu.RUnlock()
}
