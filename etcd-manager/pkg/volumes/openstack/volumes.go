/*
Copyright 2019 The Kubernetes Authors.

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

package openstack

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"github.com/gophercloud/gophercloud"
	"github.com/gophercloud/gophercloud/openstack"
	cinderv3 "github.com/gophercloud/gophercloud/openstack/blockstorage/v3/volumes"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/availabilityzones"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/extensions/volumeattach"
	"github.com/gophercloud/gophercloud/openstack/compute/v2/servers"
	"k8s.io/klog/v2"
	utilexec "k8s.io/utils/exec"
	"sigs.k8s.io/etcdadm/etcd-manager/pkg/volumes"
)

const MetadataLatest string = "http://169.254.169.254/openstack/latest/meta_data.json"

type InstanceMetadata struct {
	Name             string `json:"name"`
	ProjectID        string `json:"project_id"`
	AvailabilityZone string `json:"availability_zone"`
	Hostname         string `json:"hostname"`
	ServerID         string `json:"uuid"`
}

// OpenstackVolumes is the Volumes implementation for Openstack
type OpenstackVolumes struct {
	meta *InstanceMetadata

	matchTagKeys []string
	matchTags    map[string]string

	computeClient *gophercloud.ServiceClient
	volumeClient  *gophercloud.ServiceClient
	clusterName   string
	project       string
	instanceName  string
	internalIP    net.IP
	nameTag       string
	zone          string
	ignoreAZ      bool
}

var _ volumes.Volumes = &OpenstackVolumes{}

// NewOpenstackVolumes builds a OpenstackVolume
func NewOpenstackVolumes(clusterName string, volumeTags []string, nameTag string) (*OpenstackVolumes, error) {

	metadata, err := getLocalMetadata()
	if err != nil {
		return nil, fmt.Errorf("failed to get server metadata: %v", err)
	}

	stack := &OpenstackVolumes{
		clusterName: clusterName,
		meta:        metadata,
		matchTags:   make(map[string]string),
		nameTag:     nameTag,
	}

	for _, volumeTag := range volumeTags {
		tokens := strings.SplitN(volumeTag, "=", 2)
		if len(tokens) == 1 {
			stack.matchTagKeys = append(stack.matchTagKeys, tokens[0])
		} else {
			stack.matchTags[tokens[0]] = tokens[1]
		}
	}

	err = stack.getClients()
	if err != nil {
		return nil, fmt.Errorf("could not build OpenstackVolumes: %v", err)
	}

	err = stack.discoverTags()
	if err != nil {
		return nil, err
	}
	stack.nameTag = nameTag

	return stack, nil
}

func getLocalMetadata() (*InstanceMetadata, error) {
	var meta InstanceMetadata
	var client http.Client
	mc := NewMetricContext("metadata", "get")
	resp, err := client.Get(MetadataLatest)
	if mc.ObserveRequest(err) != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		bodyBytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		err = json.Unmarshal(bodyBytes, &meta)
		if err != nil {
			return nil, err
		}
		return &meta, nil
	}
	return nil, err
}

func getCredential() (gophercloud.AuthOptions, string, bool, error) {
	configFile, err := os.Open("/rootfs/etc/kubernetes/cloud.config")
	if err != nil {
		return gophercloud.AuthOptions{}, "", false, err
	}

	cfg, err := ReadConfig(configFile)
	if err != nil {
		return gophercloud.AuthOptions{}, "", false, err
	}

	return gophercloud.AuthOptions{
		IdentityEndpoint:            cfg.Global.AuthURL,
		Username:                    cfg.Global.Username,
		UserID:                      cfg.Global.UserID,
		Password:                    cfg.Global.Password,
		TenantID:                    cfg.Global.TenantID,
		TenantName:                  cfg.Global.TenantName,
		DomainID:                    cfg.Global.DomainID,
		DomainName:                  cfg.Global.DomainName,
		ApplicationCredentialID:     cfg.Global.ApplicationCredentialID,
		ApplicationCredentialName:   cfg.Global.ApplicationCredentialName,
		ApplicationCredentialSecret: cfg.Global.ApplicationCredentialSecret,
		AllowReauth:                 true,
	}, cfg.Global.Region, cfg.BlockStorage.IgnoreVolumeAZ, nil
}

func (stack *OpenstackVolumes) getClients() error {
	authOption, region, ignoreAZ, err := getCredential()
	if err != nil {
		return fmt.Errorf("error building openstack credentials: %v", err)
	}
	stack.ignoreAZ = ignoreAZ
	provider, err := openstack.NewClient(authOption.IdentityEndpoint)
	if err != nil {
		return fmt.Errorf("error building openstack storage client: %v", err)
	}
	ua := gophercloud.UserAgent{}
	ua.Prepend("etcd-manager")
	provider.UserAgent = ua
	klog.V(4).Infof("Using user-agent %s", ua.Join())

	err = openstack.Authenticate(provider, authOption)
	if err != nil {
		return fmt.Errorf("error authenticating openstack client: %v", err)
	}

	cinderClient, err := openstack.NewBlockStorageV3(provider, gophercloud.EndpointOpts{
		Type:   "volumev3",
		Region: region,
	})
	if err != nil {
		return fmt.Errorf("error building storage client: %v", err)
	}
	computeClient, err := openstack.NewComputeV2(provider, gophercloud.EndpointOpts{
		Type:   "compute",
		Region: region,
	})
	if err != nil {
		return fmt.Errorf("error building compute client: %v", err)
	}
	stack.volumeClient = cinderClient
	stack.computeClient = computeClient
	return nil
}

// InternalIP implements Volumes InternalIP
func (stack *OpenstackVolumes) InternalIP() net.IP {
	return stack.internalIP
}

func (stack *OpenstackVolumes) discoverTags() error {

	// Project ID
	{
		stack.project = strings.TrimSpace(stack.meta.ProjectID)
		if stack.project == "" {
			return fmt.Errorf("project metadata was empty")
		}
		klog.Infof("Found project=%q", stack.project)
	}

	// Instance Name
	{
		stack.instanceName = strings.TrimSpace(stack.meta.Name)
		if stack.instanceName == "" {
			return fmt.Errorf("instance name metadata was empty")
		}
		klog.Infof("Found instanceName=%q", stack.instanceName)
	}

	// Internal IP & zone
	{

		var extendedServer struct {
			servers.Server
			availabilityzones.ServerAvailabilityZoneExt
		}

		mc := NewMetricContext("server", "get")
		err := servers.Get(stack.computeClient, strings.TrimSpace(stack.meta.ServerID)).ExtractInto(&extendedServer)
		if mc.ObserveRequest(err) != nil {
			return fmt.Errorf("failed to retrieve server information from cloud: %v", err)
		}
		ip, err := GetServerFixedIP(extendedServer.Addresses, extendedServer.Name)
		if err != nil {
			return fmt.Errorf("error querying InternalIP from name: %v", err)
		}
		stack.internalIP = net.ParseIP(ip)
		stack.zone = extendedServer.AvailabilityZone
		klog.Infof("Found internalIP=%q and zone=%q", stack.internalIP, stack.zone)

	}

	return nil
}

func (stack *OpenstackVolumes) MyIP() (string, error) {
	if stack.internalIP == nil {
		return "", fmt.Errorf("unable to determine local IP")
	}
	return stack.internalIP.String(), nil
}

func (stack *OpenstackVolumes) buildOpenstackVolume(d *cinderv3.Volume) (*volumes.Volume, error) {
	etcdName := d.Name

	if plainText, ok := d.Metadata[stack.nameTag]; ok {
		tokens := strings.SplitN(plainText, "/", 2)
		etcdName = stack.clusterName + "-" + tokens[0]
	}

	vol := &volumes.Volume{
		ProviderID: d.ID,
		MountName:  fmt.Sprintf("master-%s", d.Name),
		EtcdName:   etcdName,
		Info: volumes.VolumeInfo{
			Description: d.Description,
		},
		Status: d.Status,
	}

	for _, attachedTo := range d.Attachments {
		vol.AttachedTo = attachedTo.ServerID
		if attachedTo.ServerID == stack.meta.ServerID {
			vol.LocalDevice = attachedTo.Device
		}
	}

	return vol, nil
}

func (stack *OpenstackVolumes) matchesTags(d *cinderv3.Volume, filterByAZ bool) bool {
	for _, k := range stack.matchTagKeys {
		_, found := d.Metadata[k]
		if !found {
			return false
		}
	}

	for k, v := range stack.matchTags {
		a, found := d.Metadata[k]
		if !found || a != v {
			return false
		}
	}

	// find volume az matching compute az
	if filterByAZ && !stack.ignoreAZ {
		if d.AvailabilityZone != stack.zone {
			return false
		}
	}

	return true
}

func (stack *OpenstackVolumes) FindVolumes() ([]*volumes.Volume, error) {
	return stack.findVolumes(true)
}

func (stack *OpenstackVolumes) findVolumes(filterByAZ bool) ([]*volumes.Volume, error) {
	var volumes []*volumes.Volume

	klog.V(2).Infof("Listing Openstack disks in %s/%s", stack.project, stack.meta.AvailabilityZone)

	mc := NewMetricContext("volumes", "list")
	pages, err := cinderv3.List(stack.volumeClient, cinderv3.ListOpts{
		TenantID: stack.project,
	}).AllPages()
	if mc.ObserveRequest(err) != nil {
		return volumes, fmt.Errorf("FindVolumes: Failed to list volumes: %v", err)
	}
	vols, err := cinderv3.ExtractVolumes(pages)
	if err != nil {
		return volumes, fmt.Errorf("FindVolumes: Failed to extract volumes: %v", err)
	}

	for _, volume := range vols {
		if !stack.matchesTags(&volume, filterByAZ) {
			continue
		}
		vol, err := stack.buildOpenstackVolume(&volume)
		if err != nil {
			klog.Warningf("skipping volume %s: %v", volume.Name, err)
			continue
		}
		volumes = append(volumes, vol)
	}

	return volumes, nil
}

func findDevicePath(volumeID string) (string, error) {
	// Build a list of candidate device paths
	candidateDeviceNodes := []string{
		// KVM
		fmt.Sprintf("virtio-%s", volumeID[:20]),
		fmt.Sprintf("virtio-%s", volumeID),
		// KVM virtio-scsi
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID[:20]),
		fmt.Sprintf("scsi-0QEMU_QEMU_HARDDISK_%s", volumeID),
		// ESXi
		fmt.Sprintf("wwn-0x%s", strings.Replace(volumeID, "-", "", -1)),
	}

	files, err := ioutil.ReadDir(volumes.PathFor("/dev/disk/by-id/"))
	if err != nil {
		return "", err
	}
	for _, f := range files {
		for _, c := range candidateDeviceNodes {
			if c == f.Name() {
				klog.V(4).Infof("Found disk attached as %q; full devicepath: %s\n", f.Name(), path.Join(volumes.PathFor("/dev/disk/by-id/"), f.Name()))
				return path.Join("/dev/disk/by-id/", f.Name()), nil
			}
		}
	}

	return "", nil
}

// probeVolume probes volume in compute
// see issue https://github.com/kubernetes/cloud-provider-openstack/issues/705
func probeVolume() error {
	// rescan scsi bus
	scsiPath := "/sys/class/scsi_host/"
	if dirs, err := ioutil.ReadDir(scsiPath); err == nil {
		for _, f := range dirs {
			name := scsiPath + f.Name() + "/scan"
			data := []byte("- - -")
			ioutil.WriteFile(name, data, 0666)
		}
	}

	executor := utilexec.New()
	args := []string{"trigger"}
	cmd := executor.Command("udevadm", args...)
	_, err := cmd.CombinedOutput()
	if err != nil {
		return err
	}
	return nil
}

// FindMountedVolume implements Volumes::FindMountedVolume
func (_ *OpenstackVolumes) FindMountedVolume(volume *volumes.Volume) (string, error) {
	// wait for 2.5min max for the volume to be attached and path found
	var backoff = volumes.Backoff{
		Duration: 6 * time.Second,
		Attempts: 25,
	}

	device := ""
	done, err := volumes.SleepUntil(backoff, func() (bool, error) {
		devpath, err := findDevicePath(volume.ProviderID)
		if err != nil {
			return false, err
		}
		if devpath != "" {
			device = devpath
			return true, nil
		}

		klog.V(2).Infof("Could not find device path for volume; scanning buses")
		if err := probeVolume(); err != nil {
			klog.V(2).Infof("Error scanning buses: %v", err)
		}

		return false, nil
	})
	if err != nil {
		// TODO: in this case we must make ensure that the volume is not attached to machine?
		return "", fmt.Errorf("failed to find device path for volume %q: %v", volume.ProviderID, err)
	}

	// If we didn't find the volume, the contract says we should return "", nil
	if !done || device == "" {
		return "", nil
	}

	if _, err := os.Stat(volumes.PathFor(device)); err != nil {
		if os.IsNotExist(err) {
			// Unexpected, but treat as not-found
			klog.Warningf("did not find device %q at expected path %q", device, volumes.PathFor(device))
			return "", nil
		}
		return "", fmt.Errorf("error checking for device %q: %v", device, err)
	}

	return device, nil
}

// AttachVolume attaches the specified volume to this instance, returning the mountpoint & nil if successful
func (stack *OpenstackVolumes) AttachVolume(volume *volumes.Volume) error {
	opts := volumeattach.CreateOpts{
		VolumeID: volume.ProviderID,
	}
	mc := NewMetricContext("volume", "attach")
	volumeAttachment, err := volumeattach.Create(stack.computeClient, stack.meta.ServerID, opts).Extract()
	if mc.ObserveRequest(err) != nil {
		return fmt.Errorf("error attaching volume %s to server %s: %v", opts.VolumeID, stack.meta.ServerID, err)
	}
	volume.LocalDevice = volumeAttachment.Device
	return nil
}

func (stack *OpenstackVolumes) InstanceName() string {
	return stack.instanceName
}
