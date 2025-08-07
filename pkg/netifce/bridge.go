package netifce

import (
	"context"

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	"github.com/samber/lo"
	"github.com/vishvananda/netlink"
)

type Bridge struct {
	Name string

	SlaveNames []string
}

type BridgeManager struct{}

func (m *BridgeManager) List(_ context.Context) (list []*Bridge, err error) {
	links, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}

	bridges := lo.Filter(links, func(link netlink.Link, index int) bool { return link.Type() == "bridge" })
	bridgeIdxMap := lo.SliceToMap(bridges, func(link netlink.Link) (int, *Bridge) {
		return link.Attrs().Index, &Bridge{
			Name: link.Attrs().Name,
		}
	})
	for _, link := range links {
		if b, exist := bridgeIdxMap[link.Attrs().MasterIndex]; exist {
			b.SlaveNames = append(b.SlaveNames, link.Attrs().Name)
		}
	}
	return lo.Values(bridgeIdxMap), nil
}

func (m *BridgeManager) Get(_ context.Context, name string) (*Bridge, error) {
	link, err := netlink.LinkByName(name)
	if err != nil {
		return nil, err
	}
	if link.Type() != "bridge" {
		return nil, errors.New("not a bridge")
	}
	bridge := &Bridge{
		Name: link.Attrs().Name,
	}
	slaves, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	for _, slave := range slaves {
		if slave.Attrs().MasterIndex == link.Attrs().Index {
			bridge.SlaveNames = append(bridge.SlaveNames, slave.Attrs().Name)
		}
	}
	return bridge, nil
}

// Reconcile 将会自动判断当前这个 Bridge 的状态 并尽量使其符合期望
func (m *BridgeManager) Reconcile(_ context.Context, bridge *Bridge) error {
	br, err := netlink.LinkByName(bridge.Name)
	if err != nil {
		if !errors.As(err, &netlink.LinkNotFoundError{}) {
			return err
		}
		link := &netlink.Bridge{
			LinkAttrs: netlink.LinkAttrs{
				Name: bridge.Name,
			},
		}
		if err = netlink.LinkAdd(link); err != nil {
			return err
		}
		if br, err = netlink.LinkByName(bridge.Name); err != nil {
			return err
		}
		if err = netlink.LinkSetUp(br); err != nil {
			return err
		}
		var errs *multierror.Error
		for _, slaveName := range bridge.SlaveNames {
			slave, err := netlink.LinkByName(slaveName)
			if err != nil {
				errs = multierror.Append(errs, err)
				continue
			}
			if err := netlink.LinkSetMaster(slave, br); err != nil {
				errs = multierror.Append(errs, err)
				continue
			}
		}
		if errs.ErrorOrNil() != nil {
			return errs.ErrorOrNil()
		}
		return nil
	}
	switch br.Attrs().OperState {
	case netlink.OperUp:
	case netlink.OperDown, netlink.OperUnknown:
		if err := netlink.LinkSetUp(br); err != nil {
			return err
		}
	default:
		return errors.Errorf("unexpect state: %s", br.Attrs().OperState)
	}

	links, err := netlink.LinkList()
	if err != nil {
		return err
	}
	var errs *multierror.Error
	for _, link := range links {
		//if link.Attrs().MasterIndex == br.Attrs().Index {
		//	if lo.Contains(bridge.SlaveNames, link.Attrs().Name) {
		//		continue
		//	}
		//	// 删除多余的桥接接口
		//	if err = netlink.LinkSetNoMaster(link); err != nil {
		//		errs = multierror.Append(errs, err)
		//	}
		//}
		// 添加需要的桥接接口
		if lo.Contains(bridge.SlaveNames, link.Attrs().Name) {
			if link.Attrs().MasterIndex != br.Attrs().Index {
				if err = netlink.LinkSetMaster(link, br); err != nil {
					errs = multierror.Append(errs, err)
				}
			}
		}
	}
	return errs.ErrorOrNil()
}

func (m *BridgeManager) Delete(_ context.Context, name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		if !errors.As(err, &netlink.LinkNotFoundError{}) {
			return err
		}
		return nil
	}
	if err = netlink.LinkSetDown(link); err != nil {
		return err
	}
	return netlink.LinkDel(link)
}
