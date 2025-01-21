package netifce

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

type Vxlan struct {
	Master string // 父接口名称
	VNI    int    // VXLAN Network Identifier
	Name   string // 接口名称
	MTU    int
	Remote *string // 对端VTEP的IP地址
	Local  *string // 本地VTEP的IP地址
	Group  *string // 组播地址
	TTL    int     // 1..255 | auto
	Port   int     // VXLAN使用的UDP端口，默认是4789。
}

type VxlanManager struct {
}

type IpShowVxlan struct {
	Ifindex     int      `json:"ifindex"`
	Ifname      string   `json:"ifname"` // 接口名称
	Flags       []string `json:"flags"`
	Mtu         int      `json:"mtu"`
	Qdisc       string   `json:"qdisc"`
	Operstate   string   `json:"operstate"`
	Linkmode    string   `json:"linkmode"`
	Group       string   `json:"group"`
	Txqlen      int      `json:"txqlen"`
	LinkType    string   `json:"link_type"`
	Address     string   `json:"address"`
	Broadcast   string   `json:"broadcast"`
	Promiscuity int      `json:"promiscuity"`
	MinMtu      int      `json:"min_mtu"`
	MaxMtu      int      `json:"max_mtu"`
	Linkinfo    struct {
		InfoKind string `json:"info_kind"`
		InfoData struct {
			Id        int    `json:"id"`
			Link      string `json:"link"` // 父接口名称
			PortRange struct {
				Low  int `json:"low"`
				High int `json:"high"`
			} `json:"port_range"`
			Port           int     `json:"port"` // 使用的UDP端口 默认4789
			Ttl            int     `json:"ttl"`
			Df             string  `json:"df"`
			Ageing         int     `json:"ageing"`
			External       bool    `json:"external"`
			Learning       bool    `json:"learning"`
			Proxy          bool    `json:"proxy"`
			Rsc            bool    `json:"rsc"`
			L2Miss         bool    `json:"l2miss"`
			L3Miss         bool    `json:"l3miss"`
			UdpCsum        bool    `json:"udp_csum"`
			UdpZeroCsum6Tx bool    `json:"udp_zero_csum6_tx"`
			UdpZeroCsum6Rx bool    `json:"udp_zero_csum6_rx"`
			RemcsumTx      bool    `json:"remcsum_tx"`
			RemcsumRx      bool    `json:"remcsum_rx"`
			Group          *string `json:"group,omitempty"`
			Remote         *string `json:"remote,omitempty"`
			Local          *string `json:"local,omitempty"`
		} `json:"info_data"`
	} `json:"linkinfo"`
	NumTxQueues int `json:"num_tx_queues"`
	NumRxQueues int `json:"num_rx_queues"`
	GsoMaxSize  int `json:"gso_max_size"`
	GsoMaxSegs  int `json:"gso_max_segs"`
}

// List 列出所有VXLAN接口
func (m *VxlanManager) List(ctx context.Context) ([]*Vxlan, error) {
	cmd := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show", "type", "vxlan")
	output, err := cmd.Output()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get VXLAN interfaces")
	}
	var interfaces []IpShowVxlan
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return nil, errors.Wrap(err, "failed to parse VXLAN interfaces")
	}
	vxlanList := make([]*Vxlan, 0, len(interfaces))
	for _, iface := range interfaces {
		vxlanList = append(vxlanList, &Vxlan{
			Master: iface.Linkinfo.InfoData.Link,
			VNI:    iface.Linkinfo.InfoData.Id,
			Name:   iface.Ifname,
			MTU:    iface.Mtu,
			Remote: iface.Linkinfo.InfoData.Remote,
			Local:  iface.Linkinfo.InfoData.Local,
			TTL:    iface.Linkinfo.InfoData.Ttl,
			Port:   iface.Linkinfo.InfoData.Port,
		})
	}
	return vxlanList, nil
}

// Get 获取指定的VXLAN接口
func (m *VxlanManager) Get(ctx context.Context, name string) (*Vxlan, error) {
	cmd := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show", name)
	output, err := cmd.Output()
	if err != nil {
		out, _ := cmd.CombinedOutput()
		return nil, errors.Wrap(errors.Wrap(err, string(out)), "failed to get VXLAN interface")
	}
	var interfaces []IpShowVxlan
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return nil, errors.Wrap(err, "failed to parse VXLAN interface")
	}
	if len(interfaces) == 0 {
		return nil, errors.New("VXLAN interface not found")
	}
	iface := interfaces[0]
	if iface.Linkinfo.InfoKind != "vxlan" {
		return nil, errors.New("interface is not a VXLAN interface")
	}
	return &Vxlan{
		Master: iface.Linkinfo.InfoData.Link,
		VNI:    iface.Linkinfo.InfoData.Id,
		Name:   iface.Ifname,
		MTU:    iface.Mtu,
		Remote: iface.Linkinfo.InfoData.Remote,
		Local:  iface.Linkinfo.InfoData.Local,
		TTL:    iface.Linkinfo.InfoData.Ttl,
		Port:   iface.Linkinfo.InfoData.Port,
	}, nil
}

// Create 创建VXLAN接口
func (m *VxlanManager) Create(ctx context.Context, vxlan *Vxlan) error {
	if vxlan == nil {
		return errors.New("vxlan cannot be nil")
	}

	// 检查参数
	if vxlan.Master == "" {
		return errors.New("master interface is required")
	}
	if vxlan.VNI < 1 || vxlan.VNI > 16777215 {
		return errors.New("invalid VNI (must be between 1 and 16777215)")
	}
	if vxlan.Name == "" {
		return errors.New("interface name is required")
	}
	//'group' requires 'dev' to be specified
	if vxlan.Group != nil && *vxlan.Group != "" && vxlan.Master == "" {
		return errors.New("'group' requires 'dev' to be specified")
	}

	// ip link add vxlan0 type vxlan id 100 group 239.1.1.1 dev ens18 dstport 4789
	// 构建创建命令
	args := []string{"link", "add",
		"name", vxlan.Name,
		"type", "vxlan",
		"id", fmt.Sprintf("%d", vxlan.VNI)}

	if vxlan.Group != nil && *vxlan.Group != "" {
		args = append(args, "group", *vxlan.Group)
	}
	if vxlan.Remote != nil && *vxlan.Remote != "" {
		args = append(args, "remote", *vxlan.Remote)
	}
	if vxlan.Local != nil && *vxlan.Local != "" {
		args = append(args, "local", *vxlan.Local)
	}
	if vxlan.TTL > 0 {
		args = append(args, "ttl", fmt.Sprintf("%d", vxlan.TTL))
	}
	args = append(args, "dev", vxlan.Master)

	args = append(args, "dstport", fmt.Sprintf("%d", vxlan.Port))
	// vxlan: destination port not specified
	// Will use Linux kernel default (non-standard value)
	// Use 'dstport 4789' to get the IANA assigned value
	// Use 'dstport 0' to get default and quiet this message

	// 创建VXLAN接口
	cmd := exec.CommandContext(ctx, "ip", args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrap(errors.Wrap(err, string(out)), "failed to create VXLAN interface")
	}

	if vxlan.MTU > 0 {
		cmd = exec.CommandContext(ctx, "ip", "link", "set", "dev", vxlan.Name, "mtu", fmt.Sprintf("%d", vxlan.MTU))
		if out, err := cmd.CombinedOutput(); err != nil {
			return errors.Wrap(errors.Wrap(err, string(out)), "failed to set VXLAN interface MTU")
		}
	}

	// 启用接口
	cmd = exec.CommandContext(ctx, "ip", "link", "set", "dev", vxlan.Name, "up")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrap(errors.Wrap(err, string(out)), "failed to set VXLAN interface up")
	}

	return nil
}

// Update 更新VXLAN接口
func (m *VxlanManager) Update(ctx context.Context, vxlan *Vxlan) error {
	if vxlan == nil {
		return errors.New("vxlan cannot be nil")
	}

	// 检查接口是否存在
	existing, err := m.Get(ctx, vxlan.Name)
	if err != nil {
		return errors.Wrap(err, "failed to get existing VXLAN interface")
	}

	// 如果MTU发生变化，更新MTU
	if vxlan.MTU > 0 && vxlan.MTU != existing.MTU {
		cmd := exec.CommandContext(ctx, "ip", "link", "set", "dev", vxlan.Name, "mtu", fmt.Sprintf("%d", vxlan.MTU))
		if out, err := cmd.CombinedOutput(); err != nil {
			return errors.Wrap(errors.Wrap(err, string(out)), "failed to update VXLAN interface MTU")
		}
	}

	return nil
}

// Delete 删除VXLAN接口
func (m *VxlanManager) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("interface name is required")
	}

	// 检查接口是否存在
	_, err := m.Get(ctx, name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil // 接口不存在，视为删除成功
		}
		return errors.Wrap(err, "failed to get VXLAN interface")
	}

	// 删除接口
	cmd := exec.CommandContext(ctx, "ip", "link", "delete", name)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrap(errors.Wrap(err, string(out)), "failed to delete VXLAN interface")
	}

	return nil
}
