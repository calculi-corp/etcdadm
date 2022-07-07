/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package azure

import (
	"context"
	"fmt"

	"github.com/Azure/azure-sdk-for-go/services/compute/mgmt/2020-06-01/compute"
	"sigs.k8s.io/etcdadm/etcd-manager/pkg/privateapi/discovery"
	"sigs.k8s.io/etcdadm/etcd-manager/pkg/volumes"
)

var _ discovery.Interface = &AzureVolumes{}

// Poll returns etcd nodes key by their IDs.
func (a *AzureVolumes) Poll() (map[string]discovery.Node, error) {
	vs, err := a.FindVolumes()
	if err != nil {
		return nil, fmt.Errorf("error finding volumes: %s", err)
	}

	instanceToVolumeMap := map[string]*volumes.Volume{}
	for _, v := range vs {
		if v.AttachedTo != "" {
			instanceToVolumeMap[v.AttachedTo] = v
		}
	}

	if len(instanceToVolumeMap) == 0 {
		return map[string]discovery.Node{}, nil
	}

	ctx := context.TODO()

	vmscaleSets, err := a.findScaleSets()
	if err != nil {
		return nil, fmt.Errorf("error finding vm scale sets: %s", err)
	}

	nodes := map[string]discovery.Node{}

	for _, vmss := range vmscaleSets {

		vms, err := a.client.listVMScaleSetVMsByName(ctx, *vmss.Name)
		if err != nil {
			return nil, fmt.Errorf("error listing VM Scale Set VMs: %s", err)
		}

		ifaces, err := a.client.listVMSSNetworkInterfacesByName(ctx, *vmss.Name)
		if err != nil {
			return nil, fmt.Errorf("error listing network interfaces: %s", err)
		}
		endpointsByVMID := map[string][]discovery.NodeEndpoint{}
		for _, iface := range ifaces {
			vmID := *iface.VirtualMachine.ID
			for _, i := range *iface.IPConfigurations {
				ep := discovery.NodeEndpoint{IP: *i.PrivateIPAddress}
				endpointsByVMID[vmID] = append(endpointsByVMID[vmID], ep)
			}
		}

		for _, vm := range vms {
			volume, ok := instanceToVolumeMap[*vm.Name]
			if !ok {
				continue
			}
			// We use the etcd node ID as the persistent
			// identifier because the data determines who we are.
			node := discovery.Node{
				ID:        volume.EtcdName,
				Endpoints: endpointsByVMID[*vm.ID],
			}
			nodes[node.ID] = node
		}
	}

	return nodes, nil
}

func (a *AzureVolumes) isScaleSetForCluster(vmss *compute.VirtualMachineScaleSet) bool {
	found := 0
	for k, v := range vmss.Tags {
		if _, ok := a.matchTagKeys[k]; ok {
			found++
		}
		if a.matchTags[k] == *v {
			found++
		}
	}
	return found == len(a.matchTagKeys)+len(a.matchTags)
}

func (a *AzureVolumes) findScaleSets() ([]*compute.VirtualMachineScaleSet, error) {
	scalesets, err := a.client.listVMScaleSets(context.TODO())
	if err != nil {
		return nil, fmt.Errorf("error listing vm scale sets: %s", err)
	}

	if err := a.client.refreshMetadata(); err != nil {
		return nil, fmt.Errorf("error refreshing metadata: %s", err)
	}

	var vs []*compute.VirtualMachineScaleSet
	for _, ss := range scalesets {
		if !a.isScaleSetForCluster(&ss) {
			continue
		}

		vs = append(vs, &ss)
	}
	return vs, nil
}
