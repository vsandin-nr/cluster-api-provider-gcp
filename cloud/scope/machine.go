/*
Copyright 2018 The Kubernetes Authors.

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

// Package scope implements scope types.
package scope

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/pkg/errors"
	"golang.org/x/mod/semver"
	"google.golang.org/api/compute/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	infrav1 "sigs.k8s.io/cluster-api-provider-gcp/api/v1beta1"
	"sigs.k8s.io/cluster-api-provider-gcp/cloud"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/controllers/noderefutil"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// MachineScopeParams defines the input parameters used to create a new MachineScope.
type MachineScopeParams struct {
	Client        client.Client
	ClusterGetter cloud.ClusterGetter
	Machine       *clusterv1.Machine
	GCPMachine    *infrav1.GCPMachine
}

// NewMachineScope creates a new MachineScope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewMachineScope(params MachineScopeParams) (*MachineScope, error) {
	if params.Client == nil {
		return nil, errors.New("client is required when creating a MachineScope")
	}
	if params.Machine == nil {
		return nil, errors.New("machine is required when creating a MachineScope")
	}
	if params.GCPMachine == nil {
		return nil, errors.New("gcp machine is required when creating a MachineScope")
	}

	helper, err := patch.NewHelper(params.GCPMachine, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	return &MachineScope{
		client:        params.Client,
		Machine:       params.Machine,
		GCPMachine:    params.GCPMachine,
		ClusterGetter: params.ClusterGetter,
		patchHelper:   helper,
	}, nil
}

// MachineScope defines a scope defined around a machine and its cluster.
type MachineScope struct {
	client        client.Client
	patchHelper   *patch.Helper
	ClusterGetter cloud.ClusterGetter
	Machine       *clusterv1.Machine
	GCPMachine    *infrav1.GCPMachine
}

// ANCHOR: MachineGetter

// Cloud returns initialized cloud.
func (m *MachineScope) Cloud() cloud.Cloud {
	return m.ClusterGetter.Cloud()
}

// Zone returns the FailureDomain for the GCPMachine.
func (m *MachineScope) Zone() string {
	if m.Machine.Spec.FailureDomain == nil {
		return ""
	}

	return *m.Machine.Spec.FailureDomain
}

// Project return the project for the GCPMachine's cluster.
func (m *MachineScope) Project() string {
	return m.ClusterGetter.Project()
}

// Name returns the GCPMachine name.
func (m *MachineScope) Name() string {
	return m.GCPMachine.Name
}

// Namespace returns the namespace name.
func (m *MachineScope) Namespace() string {
	return m.GCPMachine.Namespace
}

// ControlPlaneGroupName returns the control-plane instance group name.
func (m *MachineScope) ControlPlaneGroupName() string {
	return fmt.Sprintf("%s-%s-%s", m.ClusterGetter.Name(), infrav1.APIServerRoleTagValue, m.Zone())
}

// IsControlPlane returns true if the machine is a control plane.
func (m *MachineScope) IsControlPlane() bool {
	return util.IsControlPlaneMachine(m.Machine)
}

// Role returns the machine role from the labels.
func (m *MachineScope) Role() string {
	if util.IsControlPlaneMachine(m.Machine) {
		return "control-plane"
	}

	return "node"
}

// GetInstanceID returns the GCPMachine instance id by parsing Spec.ProviderID.
func (m *MachineScope) GetInstanceID() *string {
	parsed, err := noderefutil.NewProviderID(m.GetProviderID())
	if err != nil {
		return nil
	}

	return pointer.StringPtr(parsed.ID())
}

// GetProviderID returns the GCPMachine providerID from the spec.
func (m *MachineScope) GetProviderID() string {
	if m.GCPMachine.Spec.ProviderID != nil {
		return *m.GCPMachine.Spec.ProviderID
	}

	return ""
}

// ANCHOR_END: MachineGetter

// ANCHOR: MachineSetter

// SetProviderID sets the GCPMachine providerID in spec.
func (m *MachineScope) SetProviderID() {
	providerID := cloud.ProviderIDPrefix + path.Join(m.ClusterGetter.Project(), m.Zone(), m.Name())
	m.GCPMachine.Spec.ProviderID = pointer.StringPtr(providerID)
}

// GetInstanceStatus returns the GCPMachine instance status.
func (m *MachineScope) GetInstanceStatus() *infrav1.InstanceStatus {
	return m.GCPMachine.Status.InstanceStatus
}

// SetInstanceStatus sets the GCPMachine instance status.
func (m *MachineScope) SetInstanceStatus(v infrav1.InstanceStatus) {
	m.GCPMachine.Status.InstanceStatus = &v
}

// SetReady sets the GCPMachine Ready Status.
func (m *MachineScope) SetReady() {
	m.GCPMachine.Status.Ready = true
}

// SetFailureMessage sets the GCPMachine status failure message.
func (m *MachineScope) SetFailureMessage(v error) {
	m.GCPMachine.Status.FailureMessage = pointer.StringPtr(v.Error())
}

// SetFailureReason sets the GCPMachine status failure reason.
func (m *MachineScope) SetFailureReason(v capierrors.MachineStatusError) {
	m.GCPMachine.Status.FailureReason = &v
}

// SetAnnotation sets a key value annotation on the GCPMachine.
func (m *MachineScope) SetAnnotation(key, value string) {
	if m.GCPMachine.Annotations == nil {
		m.GCPMachine.Annotations = map[string]string{}
	}
	m.GCPMachine.Annotations[key] = value
}

// SetAddresses sets the addresses field on the GCPMachine.
func (m *MachineScope) SetAddresses(addressList []corev1.NodeAddress) {
	m.GCPMachine.Status.Addresses = addressList
}

// ANCHOR_END: MachineSetter

// ANCHOR: MachineInstanceSpec

// InstanceImageSpec returns compute instance image attched-disk spec.
func (m *MachineScope) InstanceImageSpec() *compute.AttachedDisk {
	version := ""
	if m.Machine.Spec.Version != nil {
		version = *m.Machine.Spec.Version
	}
	image := "capi-ubuntu-1804-k8s-" + strings.ReplaceAll(semver.MajorMinor(version), ".", "-")
	sourceImage := path.Join("projects", m.ClusterGetter.Project(), "global", "images", "family", image)
	if m.GCPMachine.Spec.Image != nil {
		sourceImage = *m.GCPMachine.Spec.Image
	} else if m.GCPMachine.Spec.ImageFamily != nil {
		sourceImage = *m.GCPMachine.Spec.ImageFamily
	}

	diskType := infrav1.PdStandardDiskType
	if t := m.GCPMachine.Spec.RootDeviceType; t != nil {
		diskType = *t
	}

	return &compute.AttachedDisk{
		AutoDelete: true,
		Boot:       true,
		InitializeParams: &compute.AttachedDiskInitializeParams{
			DiskSizeGb:  m.GCPMachine.Spec.RootDeviceSize,
			DiskType:    path.Join("zones", m.Zone(), "diskTypes", string(diskType)),
			SourceImage: sourceImage,
		},
	}
}

// InstanceAdditionalDiskSpec returns compute instance additional attched-disk spec.
func (m *MachineScope) InstanceAdditionalDiskSpec() []*compute.AttachedDisk {
	additionalDisks := make([]*compute.AttachedDisk, 0, len(m.GCPMachine.Spec.AdditionalDisks))
	for _, disk := range m.GCPMachine.Spec.AdditionalDisks {
		additionalDisk := &compute.AttachedDisk{
			AutoDelete: true,
			InitializeParams: &compute.AttachedDiskInitializeParams{
				DiskSizeGb: pointer.Int64PtrDerefOr(disk.Size, 30),
				DiskType:   path.Join("zones", m.Zone(), "diskTypes", string(*disk.DeviceType)),
			},
		}
		if additionalDisk.InitializeParams.DiskType == string(infrav1.LocalSsdDiskType) {
			additionalDisk.Type = "SCRATCH" // Default is PERSISTENT.
			// Override the Disk size
			additionalDisk.InitializeParams.DiskSizeGb = 375
			// For local SSDs set interface to NVME (instead of default SCSI) which is faster.
			// Most OS images would work with both NVME and SCSI disks but some may work
			// considerably faster with NVME.
			// https://cloud.google.com/compute/docs/disks/local-ssd#choose_an_interface
			additionalDisk.Interface = "NVME"
		}
		additionalDisks = append(additionalDisks, additionalDisk)
	}

	return additionalDisks
}

// InstanceNetworkInterfaceSpec returns compute network interface spec.
func (m *MachineScope) InstanceNetworkInterfaceSpec() *compute.NetworkInterface {
	networkInterface := &compute.NetworkInterface{
		Network: path.Join("projects", m.ClusterGetter.Project(), "global", "networks", m.ClusterGetter.NetworkName()),
	}

	if m.GCPMachine.Spec.PublicIP != nil && *m.GCPMachine.Spec.PublicIP {
		networkInterface.AccessConfigs = []*compute.AccessConfig{
			{
				Type: "ONE_TO_ONE_NAT",
				Name: "External NAT",
			},
		}
	}

	if m.GCPMachine.Spec.Subnet != nil {
		networkInterface.Subnetwork = path.Join("regions", m.ClusterGetter.Region(), "subnetworks", *m.GCPMachine.Spec.Subnet)
	}

	return networkInterface
}

// InstanceServiceAccountsSpec returns service-account spec.
func (m *MachineScope) InstanceServiceAccountsSpec() *compute.ServiceAccount {
	serviceAccount := &compute.ServiceAccount{
		Email: "default",
		Scopes: []string{
			compute.CloudPlatformScope,
		},
	}

	if m.GCPMachine.Spec.ServiceAccount != nil {
		serviceAccount.Email = m.GCPMachine.Spec.ServiceAccount.Email
		serviceAccount.Scopes = m.GCPMachine.Spec.ServiceAccount.Scopes
	}

	return serviceAccount
}

// InstanceAdditionalMetadataSpec returns additional metadata spec.
func (m *MachineScope) InstanceAdditionalMetadataSpec() *compute.Metadata {
	metadata := new(compute.Metadata)
	for _, additionalMetadata := range m.GCPMachine.Spec.AdditionalMetadata {
		metadata.Items = append(metadata.Items, &compute.MetadataItems{
			Key:   additionalMetadata.Key,
			Value: additionalMetadata.Value,
		})
	}

	return metadata
}

// InstanceSpec returns instance spec.
func (m *MachineScope) InstanceSpec() *compute.Instance {
	instance := &compute.Instance{
		Name:        m.Name(),
		Zone:        m.Zone(),
		MachineType: path.Join("zones", m.Zone(), "machineTypes", m.GCPMachine.Spec.InstanceType),
		Tags: &compute.Tags{
			Items: append(
				m.GCPMachine.Spec.AdditionalNetworkTags,
				fmt.Sprintf("%s-%s", m.ClusterGetter.Name(), m.Role()),
				m.ClusterGetter.Name(),
			),
		},
		Labels: infrav1.Build(infrav1.BuildParams{
			ClusterName: m.ClusterGetter.Name(),
			Lifecycle:   infrav1.ResourceLifecycleOwned,
			Role:        pointer.StringPtr(m.Role()),
			// TODO(vincepri): Check what needs to be added for the cloud provider label.
			Additional: m.ClusterGetter.AdditionalLabels().AddLabels(m.GCPMachine.Spec.AdditionalLabels),
		}),
		Scheduling: &compute.Scheduling{
			Preemptible: m.GCPMachine.Spec.Preemptible,
		},
	}

	instance.CanIpForward = true
	if m.GCPMachine.Spec.IPForwarding != nil && *m.GCPMachine.Spec.IPForwarding == infrav1.IPForwardingDisabled {
		instance.CanIpForward = false
	}

	instance.Disks = append(instance.Disks, m.InstanceImageSpec())
	instance.Disks = append(instance.Disks, m.InstanceAdditionalDiskSpec()...)
	instance.Metadata = m.InstanceAdditionalMetadataSpec()
	instance.ServiceAccounts = append(instance.ServiceAccounts, m.InstanceServiceAccountsSpec())
	instance.NetworkInterfaces = append(instance.NetworkInterfaces, m.InstanceNetworkInterfaceSpec())
	return instance
}

// ANCHOR_END: MachineInstanceSpec

// GetBootstrapData returns the bootstrap data from the secret in the Machine's bootstrap.dataSecretName.
func (m *MachineScope) GetBootstrapData() (string, error) {
	if m.Machine.Spec.Bootstrap.DataSecretName == nil {
		return "", errors.New("error retrieving bootstrap data: linked Machine's bootstrap.dataSecretName is nil")
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: m.Namespace(), Name: *m.Machine.Spec.Bootstrap.DataSecretName}
	if err := m.client.Get(context.TODO(), key, secret); err != nil {
		return "", errors.Wrapf(err, "failed to retrieve bootstrap data secret for GCPMachine %s/%s", m.Namespace(), m.Name())
	}

	value, ok := secret.Data["value"]
	if !ok {
		return "", errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return string(value), nil
}

// PatchObject persists the cluster configuration and status.
func (m *MachineScope) PatchObject() error {
	return m.patchHelper.Patch(context.TODO(), m.GCPMachine)
}

// Close closes the current scope persisting the cluster configuration and status.
func (m *MachineScope) Close() error {
	return m.PatchObject()
}
