package netifce

import (
	"context"
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"github.com/vishvananda/netlink"
	"testing"
)

func TestBridgeManager_Reconcile(t *testing.T) {
	if testing.Short() {
		t.Skip("skip for short test")
	}

	manager := BridgeManager{}

	slaveIfce1Name := "ki.slave"
	err := netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: slaveIfce1Name}})
	assert.NoError(t, err)
	slaveIfce1, err := netlink.LinkByName(slaveIfce1Name)
	assert.NoError(t, err)
	err = netlink.LinkSetUp(slaveIfce1)
	assert.NoError(t, err)

	slaveIfce2Name := "ki.slave2"
	err = netlink.LinkAdd(&netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Name: slaveIfce2Name}})
	assert.NoError(t, err)
	slaveIfce2, err := netlink.LinkByName(slaveIfce2Name)
	assert.NoError(t, err)
	err = netlink.LinkSetUp(slaveIfce2)
	assert.NoError(t, err)

	bridgeName := "ki.bridge1"

	err = manager.Reconcile(context.Background(), &Bridge{
		Name:       bridgeName,
		SlaveNames: []string{slaveIfce1Name},
	})
	assert.NoError(t, err)

	list, err := manager.List(context.Background())
	if err != nil {
		return
	}
	if !lo.SomeBy(list, func(item *Bridge) bool {
		return item.Name == bridgeName
	}) {
		t.Error("bridge not created")
	}

	err = manager.Reconcile(context.Background(), &Bridge{
		Name:       bridgeName,
		SlaveNames: []string{slaveIfce1Name, slaveIfce2Name},
	})
	assert.NoError(t, err)

	err = manager.Delete(context.Background(), slaveIfce1Name)
	assert.NoError(t, err)
	err = manager.Delete(context.Background(), slaveIfce2Name)
	assert.NoError(t, err)

	err = manager.Delete(context.Background(), bridgeName)
	assert.NoError(t, err)

	list, err = manager.List(context.Background())
	if err != nil {
		return
	}
	if lo.SomeBy(list, func(item *Bridge) bool {
		return item.Name == bridgeName
	}) {
		t.Error("bridge not deleted")
	}
}
