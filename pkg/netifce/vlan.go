package netifce

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"

	"github.com/pkg/errors"
)

type Vlan struct {
	Master string // 父接口名称
	Id     int    // vlan id
	Name   string // 接口名称
	MTU    int
}

type VlanManager struct {
}

type IpShowVlan struct {
	Ifindex     int      `json:"ifindex"`
	Link        string   `json:"link"`   // 父接口名称
	Ifname      string   `json:"ifname"` // 接口名称
	Flags       []string `json:"flags"`  // "BROADCAST","MULTICAST","UP","LOWER_UP"
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
			Protocol string   `json:"protocol"`
			Id       int      `json:"id"`
			Flags    []string `json:"flags"`
		} `json:"info_data"`
	} `json:"linkinfo"`
	Inet6AddrGenMode string `json:"inet6_addr_gen_mode"`
	NumTxQueues      int    `json:"num_tx_queues"`
	NumRxQueues      int    `json:"num_rx_queues"`
	GsoMaxSize       int    `json:"gso_max_size"`
	GsoMaxSegs       int    `json:"gso_max_segs"`
}

// List 列出所有VLAN接口
func (m *VlanManager) List(ctx context.Context) ([]*Vlan, error) {
	cmd := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show", "type", "vlan")
	output, err := cmd.Output()
	if err != nil {

		return nil, errors.Wrap(err, "failed to get VLAN interfaces")
	}
	var interfaces []IpShowVlan
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return nil, errors.Wrap(err, "failed to parse VLAN interfaces")
	}
	vlanList := make([]*Vlan, 0, len(interfaces))
	for _, iface := range interfaces {
		vlanList = append(vlanList, &Vlan{
			Master: iface.Link,
			Id:     iface.Linkinfo.InfoData.Id,
			Name:   iface.Ifname,
			MTU:    iface.Mtu,
		})
	}
	return vlanList, nil
}

// Get 获取指定的VLAN接口
func (m *VlanManager) Get(ctx context.Context, name string) (*Vlan, error) {
	cmd := exec.CommandContext(ctx, "ip", "-j", "-d", "link", "show", name)
	output, err := cmd.Output()
	if err != nil {
		out, _ := cmd.CombinedOutput()
		return nil, errors.Wrap(errors.Wrap(err, string(out)), "failed to get VLAN interface")
	}
	var interfaces []IpShowVlan
	if err := json.Unmarshal(output, &interfaces); err != nil {
		return nil, errors.Wrap(err, "failed to parse VLAN interface")
	}
	if len(interfaces) == 0 {
		return nil, errors.New("VLAN interface not found")
	}
	iface := interfaces[0]
	if iface.Linkinfo.InfoKind != "vlan" {
		return nil, errors.New("interface is not a VLAN interface")
	}
	return &Vlan{
		Master: iface.Link,
		Id:     iface.Linkinfo.InfoData.Id,
		Name:   iface.Ifname,
		MTU:    iface.Mtu,
	}, nil
}

// Create 创建VLAN接口
func (m *VlanManager) Create(ctx context.Context, vlan *Vlan) error {
	if vlan == nil {
		return errors.New("vlan cannot be nil")
	}

	// 检查参数
	if vlan.Master == "" {
		return errors.New("master interface is required")
	}
	if vlan.Id < 1 || vlan.Id > 4094 {
		return errors.New("invalid VLAN ID (must be between 1 and 4094)")
	}
	if vlan.Name == "" {
		return errors.New("interface name is required")
	}

	// 构建创建命令
	args := []string{"link", "add",
		"link", vlan.Master,
		"name", vlan.Name,
		"type", "vlan",
		"id", fmt.Sprintf("%d", vlan.Id)}
	if vlan.MTU > 0 {
		args = append(args, "mtu", fmt.Sprintf("%d", vlan.MTU))
	}

	// 创建VLAN接口
	cmd := exec.CommandContext(ctx, "ip", args...)
	if err := cmd.Run(); err != nil {
		out, _ := cmd.CombinedOutput()
		return errors.Wrap(errors.Wrap(err, string(out)), "failed to create VLAN interface")
	}

	// 启用接口
	cmd = exec.CommandContext(ctx, "ip", "link", "set", "dev", vlan.Name, "up")
	if err := cmd.Run(); err != nil {
		out, _ := cmd.CombinedOutput()
		return errors.Wrap(errors.Wrap(err, string(out)), "failed to set VLAN interface up")
	}

	return nil
}

// Update 更新VLAN接口
func (m *VlanManager) Update(ctx context.Context, vlan *Vlan) error {
	if vlan == nil {
		return errors.New("vlan cannot be nil")
	}

	// 检查接口是否存在
	existing, err := m.Get(ctx, vlan.Name)
	if err != nil {
		return errors.Wrap(err, "failed to get existing VLAN interface")
	}

	// 如果MTU发生变化，更新MTU
	if vlan.MTU > 0 && vlan.MTU != existing.MTU {
		cmd := exec.CommandContext(ctx, "ip", "link", "set", "dev", vlan.Name, "mtu", fmt.Sprintf("%d", vlan.MTU))
		if err = cmd.Run(); err != nil {
			out, _ := cmd.CombinedOutput()
			return errors.Wrap(errors.Wrap(err, string(out)), "failed to update VLAN interface MTU")
		}
	}

	return nil
}

// Delete 删除VLAN接口
func (m *VlanManager) Delete(ctx context.Context, name string) error {
	if name == "" {
		return errors.New("interface name is required")
	}

	// 检查接口是否存在
	_, err := m.Get(ctx, name)
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return nil // 接口不存在，视为删除成功
		}
		return errors.Wrap(err, "failed to get VLAN interface")
	}

	// 删除接口
	cmd := exec.CommandContext(ctx, "ip", "link", "delete", name)
	if err = cmd.Run(); err != nil {
		out, _ := cmd.CombinedOutput()
		return errors.Wrap(errors.Wrap(err, string(out)), "failed to delete VLAN interface")
	}

	return nil
}
