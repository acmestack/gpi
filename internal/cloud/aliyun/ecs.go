package aliyun

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
)

type regionResp struct {
	Regions struct {
		Region []struct {
			RegionId string `json:"RegionId"`
		} `json:"Region"`
	} `json:"Regions"`
}

func (c *Client) DescribeRegions(ctx context.Context) ([]string, error) {
	var out regionResp
	if err := c.call(ctx, "DescribeRegions", nil, &out); err != nil {
		return nil, err
	}
	regions := make([]string, 0, len(out.Regions.Region))
	for _, r := range out.Regions.Region {
		regions = append(regions, r.RegionId)
	}
	return regions, nil
}

type zonesResp struct {
	Zones struct {
		Zone []struct {
			ZoneId string `json:"ZoneId"`
		} `json:"Zone"`
	} `json:"Zones"`
}

func (c *Client) DescribeZones(ctx context.Context, region string) ([]string, error) {
	var out zonesResp
	if err := c.call(ctx, "DescribeZones", map[string]string{"RegionId": region}, &out); err != nil {
		return nil, err
	}
	zones := make([]string, 0, len(out.Zones.Zone))
	for _, z := range out.Zones.Zone {
		zones = append(zones, z.ZoneId)
	}
	return zones, nil
}

type createKeyPairResp struct {
	KeyPairName    string `json:"KeyPairName"`
	PrivateKeyBody string `json:"PrivateKeyBody"`
}

func (c *Client) CreateKeyPair(ctx context.Context, region, name string) (string, error) {
	var out createKeyPairResp
	if err := c.call(ctx, "CreateKeyPair", map[string]string{
		"RegionId":    region,
		"KeyPairName": name,
	}, &out); err != nil {
		return "", err
	}
	return out.PrivateKeyBody, nil
}

func (c *Client) DeleteKeyPair(ctx context.Context, region, name string) error {
	return c.call(ctx, "DeleteKeyPair", map[string]string{
		"RegionId":    region,
		"KeyPairName": name,
	}, nil)
}

type createVpcResp struct {
	VpcId string `json:"VpcId"`
}

func (c *Client) CreateVpc(ctx context.Context, region, cidr, name string) (string, error) {
	var out createVpcResp
	if err := c.call(ctx, "CreateVpc", map[string]string{
		"RegionId":  region,
		"CidrBlock": cidr,
		"VpcName":   name,
	}, &out); err != nil {
		return "", err
	}
	return out.VpcId, nil
}

type vpcItem struct {
	VpcId        string `json:"VpcId"`
	VpcName      string `json:"VpcName"`
	Status       string `json:"Status"`
	IsDefault    bool   `json:"IsDefault"`
	CreationTime string `json:"CreationTime"`
}

type vpcResp struct {
	Vpcs struct {
		Vpc []vpcItem `json:"Vpc"`
	} `json:"Vpcs"`
}

// DescribeVpcs lists VPCs in a region, optionally filtering to the default VPC.
func (c *Client) DescribeVpcs(ctx context.Context, region string, onlyDefault bool) ([]vpcItem, error) {
	params := map[string]string{"RegionId": region}
	if onlyDefault {
		params["IsDefault"] = "true"
	}
	var out vpcResp
	if err := c.call(ctx, "DescribeVpcs", params, &out); err != nil {
		return nil, err
	}
	return out.Vpcs.Vpc, nil
}

type createVSwitchResp struct {
	VSwitchId string `json:"VSwitchId"`
}

func (c *Client) CreateVSwitch(ctx context.Context, region, vpcID, zone, cidr, name string) (string, error) {
	var out createVSwitchResp
	if err := c.call(ctx, "CreateVSwitch", map[string]string{
		"RegionId":    region,
		"VpcId":       vpcID,
		"ZoneId":      zone,
		"CidrBlock":   cidr,
		"VSwitchName": name,
	}, &out); err != nil {
		return "", err
	}
	return out.VSwitchId, nil
}

type vSwitchItem struct {
	VSwitchId        string `json:"VSwitchId"`
	VSwitchName      string `json:"VSwitchName"`
	ZoneId           string `json:"ZoneId"`
	CidrBlock        string `json:"CidrBlock"`
	VpcId            string `json:"VpcId"`
	Status           string `json:"Status"`
	AvailableIPCount int    `json:"AvailableIpAddressCount"`
}

type vSwitchResp struct {
	VSwitches struct {
		VSwitch []vSwitchItem `json:"VSwitch"`
	} `json:"VSwitches"`
}

func (c *Client) ListVSwitches(ctx context.Context, region, vpcID string) ([]cloud.VSwitch, error) {
	var out vSwitchResp
	if err := c.call(ctx, "DescribeVSwitches", map[string]string{
		"RegionId": region,
		"VpcId":    vpcID,
	}, &out); err != nil {
		return nil, err
	}
	result := make([]cloud.VSwitch, 0, len(out.VSwitches.VSwitch))
	for _, v := range out.VSwitches.VSwitch {
		result = append(result, cloud.VSwitch{
			ID:    v.VSwitchId,
			Name:  v.VSwitchName,
			Zone:  v.ZoneId,
			CIDR:  v.CidrBlock,
			VPCID: v.VpcId,
		})
	}
	return result, nil
}

type createSgResp struct {
	SecurityGroupId string `json:"SecurityGroupId"`
}

func (c *Client) CreateSecurityGroup(ctx context.Context, region, vpcID, name, desc string) (string, error) {
	var out createSgResp
	if err := c.call(ctx, "CreateSecurityGroup", map[string]string{
		"RegionId":          region,
		"VpcId":             vpcID,
		"SecurityGroupName": name,
		"Description":       desc,
	}, &out); err != nil {
		return "", err
	}
	return out.SecurityGroupId, nil
}

func (c *Client) AuthorizeSecurityGroup(ctx context.Context, region, groupID string, portFrom, portTo int, protocol string) error {
	return c.call(ctx, "AuthorizeSecurityGroup", map[string]string{
		"RegionId":        region,
		"SecurityGroupId": groupID,
		"IpProtocol":      protocol,
		"PortRange":       fmt.Sprintf("%d/%d", portFrom, portTo),
		"SourceCidrIp":    "0.0.0.0/0",
	}, nil)
}

type runInstancesResp struct {
	InstanceIdSets struct {
		InstanceIdSet []string `json:"InstanceIdSet"`
	} `json:"InstanceIdSets"`
}

func (c *Client) RunInstances(ctx context.Context, spec *cloud.LaunchSpec) ([]string, error) {
	params := map[string]string{
		"RegionId":                spec.Region,
		"ImageId":                 spec.ImageID,
		"InstanceType":            spec.InstanceType,
		"InstanceName":            spec.NamePrefix,
		"Amount":                  strconv.Itoa(spec.NumNodes),
		"InstanceChargeType":      "PostPaid",
		"KeyPairName":             spec.KeyName,
		"SecurityGroupId":         spec.SecurityGroupID,
		"ClientToken":             newNonce(),
		"InternetMaxBandwidthOut": "100",
	}
	if spec.VSwitchID != "" {
		params["VSwitchId"] = spec.VSwitchID
	}
	if spec.Zone != "" {
		params["ZoneId"] = spec.Zone
	}
	if spec.DiskSizeGiB > 0 {
		params["SystemDisk.Size"] = strconv.Itoa(spec.DiskSizeGiB)
	}
	if spec.SpotStrategy != "" {
		params["SpotStrategy"] = spec.SpotStrategy
		if spec.SpotPriceLimit > 0 {
			params["SpotPriceLimit"] = strconv.FormatFloat(spec.SpotPriceLimit, 'f', -1, 64)
		}
	}
	if spec.UserData != "" {
		params["UserData"] = base64.StdEncoding.EncodeToString([]byte(spec.UserData))
	}
	i := 1
	for k, v := range spec.Tags {
		params[fmt.Sprintf("Tag.%d.Key", i)] = k
		params[fmt.Sprintf("Tag.%d.Value", i)] = v
		i++
	}
	var out runInstancesResp
	if err := c.call(ctx, "RunInstances", params, &out); err != nil {
		return nil, err
	}
	return out.InstanceIdSets.InstanceIdSet, nil
}

type instanceItem struct {
	InstanceId      string `json:"InstanceId"`
	InstanceName    string `json:"InstanceName"`
	Status          string `json:"Status"`
	InstanceType    string `json:"InstanceType"`
	RegionId        string `json:"RegionId"`
	ZoneId          string `json:"ZoneId"`
	CreationTime    string `json:"CreationTime"`
	PublicIpAddress struct {
		IpAddress []string `json:"IpAddress"`
	} `json:"PublicIpAddress"`
	EipAddress struct {
		IpAddress string `json:"IpAddress"`
	} `json:"EipAddress"`
	VpcAttributes struct {
		PrivateIpAddress struct {
			IpAddress []string `json:"IpAddress"`
		} `json:"PrivateIpAddress"`
	} `json:"VpcAttributes"`
}

type instancesResp struct {
	Instances struct {
		Instance []instanceItem `json:"Instance"`
	} `json:"Instances"`
	TotalCount int `json:"TotalCount"`
}

func (c *Client) DescribeInstances(ctx context.Context, region string, ids []string, namePrefix string, page int) ([]cloud.Instance, error) {
	params := map[string]string{
		"RegionId":   region,
		"PageSize":   "100",
		"PageNumber": strconv.Itoa(page),
	}
	if len(ids) > 0 {
		params["InstanceIds"] = `["` + strings.Join(ids, `","`) + `"]`
	}
	if namePrefix != "" {
		params["InstanceName"] = namePrefix
	}
	var out instancesResp
	if err := c.call(ctx, "DescribeInstances", params, &out); err != nil {
		return nil, err
	}
	result := make([]cloud.Instance, 0, len(out.Instances.Instance))
	for _, item := range out.Instances.Instance {
		publicIP := item.EipAddress.IpAddress
		if publicIP == "" && len(item.PublicIpAddress.IpAddress) > 0 {
			publicIP = item.PublicIpAddress.IpAddress[0]
		}
		privateIP := ""
		if len(item.VpcAttributes.PrivateIpAddress.IpAddress) > 0 {
			privateIP = item.VpcAttributes.PrivateIpAddress.IpAddress[0]
		}
		result = append(result, cloud.Instance{
			ID:           item.InstanceId,
			Name:         item.InstanceName,
			PublicIP:     publicIP,
			PrivateIP:    privateIP,
			Status:       mapStatus(item.Status),
			InstanceType: item.InstanceType,
			Region:       item.RegionId,
			Zone:         item.ZoneId,
			CreatedAt:    parseTime(item.CreationTime),
		})
	}
	return result, nil
}

func (c *Client) StartInstance(ctx context.Context, region, id string) error {
	return c.call(ctx, "StartInstance", map[string]string{
		"RegionId":   region,
		"InstanceId": id,
	}, nil)
}

func (c *Client) StopInstance(ctx context.Context, region, id string) error {
	return c.call(ctx, "StopInstance", map[string]string{
		"RegionId":   region,
		"InstanceId": id,
		"ForceStop":  "true",
	}, nil)
}

func (c *Client) DeleteInstance(ctx context.Context, region, id string) error {
	return c.call(ctx, "DeleteInstance", map[string]string{
		"RegionId":   region,
		"InstanceId": id,
		"Force":      "true",
	}, nil)
}

type imagesResp struct {
	Images struct {
		Image []struct {
			ImageId         string `json:"ImageId"`
			ImageName       string `json:"ImageName"`
			CreationTime    string `json:"CreationTime"`
			ImageOwnerAlias string `json:"ImageOwnerAlias"`
			Status          string `json:"Status"`
		} `json:"Image"`
	} `json:"Images"`
}

func (c *Client) GetImage(ctx context.Context, region, platform string) (string, error) {
	var candidates []struct {
		ImageId      string
		CreationTime string
	}
	for page := 1; page <= 3; page++ {
		var out imagesResp
		if err := c.call(ctx, "DescribeImages", map[string]string{
			"RegionId":   region,
			"PageSize":   "100",
			"PageNumber": strconv.Itoa(page),
		}, &out); err != nil {
			return "", err
		}
		for _, img := range out.Images.Image {
			if img.Status != "Available" || img.ImageOwnerAlias != "system" {
				continue
			}
			name := strings.ToLower(img.ImageName)
			if strings.Contains(name, platform) && strings.Contains(name, "x64") {
				candidates = append(candidates, struct {
					ImageId      string
					CreationTime string
				}{img.ImageId, img.CreationTime})
			}
		}
		if len(out.Images.Image) < 100 {
			break
		}
	}
	if len(candidates) == 0 {
		return "", fmt.Errorf("no %s system image found in %s", platform, region)
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		return candidates[i].CreationTime > candidates[j].CreationTime
	})
	return candidates[0].ImageId, nil
}

func mapStatus(s string) cloud.InstanceStatus {
	switch s {
	case "Pending":
		return cloud.StatusPending
	case "Starting":
		return cloud.StatusStarting
	case "Running":
		return cloud.StatusRunning
	case "Stopping":
		return cloud.StatusStopping
	case "Stopped":
		return cloud.StatusStopped
	case "Deleted":
		return cloud.StatusTerminated
	default:
		return cloud.StatusUnknown
	}
}

func parseTime(s string) int64 {
	if s == "" {
		return 0
	}
	if t, err := time.Parse("2006-01-02T15:04:05Z", s); err == nil {
		return t.Unix()
	}
	return 0
}
