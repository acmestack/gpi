package aws

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/acmestack/gpi/internal/cloud"
)

type regionsResp struct {
	RegionInfo struct {
		Item []struct {
			RegionName string `xml:"regionName"`
		} `xml:"item"`
	} `xml:"regionInfo"`
}

func (c *Client) DescribeRegions(ctx context.Context) ([]string, error) {
	var out regionsResp
	if err := c.call(ctx, "DescribeRegions", nil, &out); err != nil {
		return nil, err
	}
	regions := make([]string, 0, len(out.RegionInfo.Item))
	for _, r := range out.RegionInfo.Item {
		regions = append(regions, r.RegionName)
	}
	return regions, nil
}

type zonesResp struct {
	AvailabilityZoneInfo struct {
		Item []struct {
			ZoneName  string `xml:"zoneName"`
			ZoneState string `xml:"zoneState"`
		} `xml:"item"`
	} `xml:"availabilityZoneInfo"`
}

func (c *Client) DescribeZones(ctx context.Context, region string) ([]string, error) {
	var out zonesResp
	if err := c.call(ctx, "DescribeAvailabilityZones", map[string]string{
		"Filter.1.Name":    "state",
		"Filter.1.Value.1": "available",
	}, &out); err != nil {
		return nil, err
	}
	zones := make([]string, 0, len(out.AvailabilityZoneInfo.Item))
	for _, z := range out.AvailabilityZoneInfo.Item {
		zones = append(zones, z.ZoneName)
	}
	return zones, nil
}

type createKeyPairResp struct {
	KeyName        string `xml:"keyName"`
	KeyFingerprint string `xml:"keyFingerprint"`
	KeyMaterial    string `xml:"keyMaterial"`
}

func (c *Client) CreateKeyPair(ctx context.Context, name string) (string, error) {
	var out createKeyPairResp
	if err := c.call(ctx, "CreateKeyPair", map[string]string{"KeyName": name}, &out); err != nil {
		return "", err
	}
	return out.KeyMaterial, nil
}

func (c *Client) DeleteKeyPair(ctx context.Context, name string) error {
	return c.call(ctx, "DeleteKeyPair", map[string]string{"KeyName": name}, nil)
}

type runInstancesResp struct {
	InstancesSet struct {
		Item []struct {
			InstanceId string `xml:"instanceId"`
		} `xml:"item"`
	} `xml:"instancesSet"`
}

func (c *Client) RunInstances(ctx context.Context, spec *cloud.LaunchSpec) ([]string, error) {
	params := map[string]string{
		"ImageId":                         spec.ImageID,
		"InstanceType":                    spec.InstanceType,
		"MinCount":                        strconv.Itoa(spec.NumNodes),
		"MaxCount":                        strconv.Itoa(spec.NumNodes),
		"KeyName":                         spec.KeyName,
		"ClientToken":                     newNonce(),
		"TagSpecification.1.ResourceType": "instance",
		"TagSpecification.1.Tag.1.Key":    "Name",
		"TagSpecification.1.Tag.1.Value":  spec.NamePrefix,
	}
	if spec.VSwitchID != "" {
		params["SubnetId"] = spec.VSwitchID
	} else if spec.SecurityGroupID != "" {
		params["SecurityGroupId.1"] = spec.SecurityGroupID
	}
	if spec.SpotStrategy != "" {
		params["InstanceMarketOptions.0.MarketType"] = "spot"
		params["InstanceMarketOptions.0.SpotOptions.0.SpotInstanceType"] = "one-time"
	}
	if spec.DiskSizeGiB > 0 {
		params["BlockDeviceMappings.0.DeviceName"] = "/dev/sda1"
		params["BlockDeviceMappings.0.Ebs.VolumeSize"] = strconv.Itoa(spec.DiskSizeGiB)
		params["BlockDeviceMappings.0.Ebs.DeleteOnTermination"] = "true"
	}
	if spec.UserData != "" {
		params["UserData"] = base64.StdEncoding.EncodeToString([]byte(spec.UserData))
	}
	idx := 2
	for k, v := range spec.Tags {
		params[fmt.Sprintf("TagSpecification.1.Tag.%d.Key", idx)] = k
		params[fmt.Sprintf("TagSpecification.1.Tag.%d.Value", idx)] = v
		idx++
	}
	var out runInstancesResp
	if err := c.call(ctx, "RunInstances", params, &out); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(out.InstancesSet.Item))
	for _, item := range out.InstancesSet.Item {
		ids = append(ids, item.InstanceId)
	}
	return ids, nil
}

type instanceItem struct {
	InstanceId       string `xml:"instanceId"`
	InstanceType     string `xml:"instanceType"`
	LaunchTime       string `xml:"launchTime"`
	PublicIpAddress  string `xml:"publicIpAddress"`
	PrivateIpAddress string `xml:"privateIpAddress"`
	InstanceState    struct {
		Name string `xml:"name"`
	} `xml:"instanceState"`
	Placement struct {
		AvailabilityZone string `xml:"availabilityZone"`
	} `xml:"placement"`
	TagSet struct {
		Item []struct {
			Key   string `xml:"key"`
			Value string `xml:"value"`
		} `xml:"item"`
	} `xml:"tagSet"`
}

type instancesResp struct {
	ReservationSet struct {
		Item []struct {
			InstancesSet struct {
				Item []instanceItem `xml:"item"`
			} `xml:"instancesSet"`
		} `xml:"item"`
	} `xml:"reservationSet"`
	NextToken string `xml:"nextToken"`
}

func (c *Client) DescribeInstances(ctx context.Context, ids []string, namePrefix string) ([]cloud.Instance, error) {
	var all []cloud.Instance
	params := map[string]string{}
	if len(ids) > 0 {
		for i, id := range ids {
			params[fmt.Sprintf("InstanceId.%d", i+1)] = id
		}
	}
	if namePrefix != "" {
		params["Filter.1.Name"] = "tag:Name"
		params["Filter.1.Value.1"] = namePrefix
	}
	for {
		var out instancesResp
		if err := c.call(ctx, "DescribeInstances", params, &out); err != nil {
			return nil, err
		}
		for _, res := range out.ReservationSet.Item {
			for _, item := range res.InstancesSet.Item {
				all = append(all, cloud.Instance{
					ID:           item.InstanceId,
					Name:         tagName(item.TagSet),
					PublicIP:     item.PublicIpAddress,
					PrivateIP:    item.PrivateIpAddress,
					Status:       mapStatus(item.InstanceState.Name),
					InstanceType: item.InstanceType,
					Region:       c.region,
					Zone:         item.Placement.AvailabilityZone,
					CreatedAt:    parseTime(item.LaunchTime),
				})
			}
		}
		if out.NextToken == "" {
			break
		}
		params["NextToken"] = out.NextToken
	}
	return all, nil
}

func tagName(tagSet struct {
	Item []struct {
		Key   string `xml:"key"`
		Value string `xml:"value"`
	} `xml:"item"`
}) string {
	for _, item := range tagSet.Item {
		if item.Key == "Name" {
			return item.Value
		}
	}
	return ""
}

// Name returns the resource's "Name" tag value.
func (v vpcItem) Name() string {
	return tagName(v.TagSet)
}

// Name returns the resource's "Name" tag value.
func (s subnetItem) Name() string {
	return tagName(s.TagSet)
}

func (c *Client) StartInstance(ctx context.Context, id string) error {
	return c.call(ctx, "StartInstances", map[string]string{"InstanceId.1": id}, nil)
}

func (c *Client) StopInstance(ctx context.Context, id string) error {
	return c.call(ctx, "StopInstances", map[string]string{
		"InstanceId.1": id,
		"Force":        "true",
	}, nil)
}

func (c *Client) TerminateInstance(ctx context.Context, id string) error {
	return c.call(ctx, "TerminateInstances", map[string]string{"InstanceId.1": id}, nil)
}

type createSecurityGroupResp struct {
	GroupId string `xml:"groupId"`
}

func (c *Client) CreateSecurityGroup(ctx context.Context, groupName, desc, vpcID string) (string, error) {
	params := map[string]string{
		"GroupName":        groupName,
		"GroupDescription": desc,
	}
	if vpcID != "" {
		params["VpcId"] = vpcID
	}
	var out createSecurityGroupResp
	if err := c.call(ctx, "CreateSecurityGroup", params, &out); err != nil {
		return "", err
	}
	return out.GroupId, nil
}

func (c *Client) AuthorizeSecurityGroup(ctx context.Context, groupID string, portFrom, portTo int, protocol string) error {
	return c.call(ctx, "AuthorizeSecurityGroupIngress", map[string]string{
		"GroupId":                           groupID,
		"IpPermissions.0.IpProtocol":        protocol,
		"IpPermissions.0.FromPort":          strconv.Itoa(portFrom),
		"IpPermissions.0.ToPort":            strconv.Itoa(portTo),
		"IpPermissions.0.IpRanges.1.CidrIp": "0.0.0.0/0",
	}, nil)
}

type securityGroupItem struct {
	GroupId    string `xml:"groupId"`
	GroupName  string `xml:"groupName"`
	VpcId      string `xml:"vpcId"`
	IpProtocol string `xml:"ipPermissions"`
}

type securityGroupsResp struct {
	SecurityGroupInfo struct {
		Item []securityGroupItem `xml:"item"`
	} `xml:"securityGroupInfo"`
}

func (c *Client) DescribeSecurityGroups(ctx context.Context) ([]securityGroupItem, error) {
	var out securityGroupsResp
	if err := c.call(ctx, "DescribeSecurityGroups", nil, &out); err != nil {
		return nil, err
	}
	return out.SecurityGroupInfo.Item, nil
}

type vpcItem struct {
	VpcId     string `xml:"vpcId"`
	CidrBlock string `xml:"cidrBlock"`
	IsDefault bool   `xml:"isDefault"`
	TagSet    struct {
		Item []struct {
			Key   string `xml:"key"`
			Value string `xml:"value"`
		} `xml:"item"`
	} `xml:"tagSet"`
}

type vpcsResp struct {
	VpcSet struct {
		Item []vpcItem `xml:"item"`
	} `xml:"vpcSet"`
}

func (c *Client) DescribeVpcs(ctx context.Context) ([]vpcItem, error) {
	var out vpcsResp
	if err := c.call(ctx, "DescribeVpcs", nil, &out); err != nil {
		return nil, err
	}
	return out.VpcSet.Item, nil
}

type createVpcResp struct {
	VpcId string `xml:"vpcId"`
}

func (c *Client) CreateVpc(ctx context.Context, cidr, name string) (string, error) {
	params := map[string]string{"CidrBlock": cidr}
	if name != "" {
		params["TagSpecification.1.ResourceType"] = "vpc"
		params["TagSpecification.1.Tag.1.Key"] = "Name"
		params["TagSpecification.1.Tag.1.Value"] = name
	}
	var out createVpcResp
	if err := c.call(ctx, "CreateVpc", params, &out); err != nil {
		return "", err
	}
	return out.VpcId, nil
}

type createIgwResp struct {
	InternetGatewayId string `xml:"internetGatewayId"`
}

func (c *Client) CreateInternetGateway(ctx context.Context, name string) (string, error) {
	var out createIgwResp
	if err := c.call(ctx, "CreateInternetGateway", nil, &out); err != nil {
		return "", err
	}
	return out.InternetGatewayId, nil
}

func (c *Client) AttachInternetGateway(ctx context.Context, igwID, vpcID string) error {
	return c.call(ctx, "AttachInternetGateway", map[string]string{
		"InternetGatewayId": igwID,
		"VpcId":             vpcID,
	}, nil)
}

type createRouteTableResp struct {
	RouteTableId string `xml:"routeTableId"`
}

func (c *Client) CreateRouteTable(ctx context.Context, vpcID string) (string, error) {
	var out createRouteTableResp
	if err := c.call(ctx, "CreateRouteTable", map[string]string{"VpcId": vpcID}, &out); err != nil {
		return "", err
	}
	return out.RouteTableId, nil
}

func (c *Client) CreateRoute(ctx context.Context, routeTableID, igwID string) error {
	return c.call(ctx, "CreateRoute", map[string]string{
		"RouteTableId":         routeTableID,
		"DestinationCidrBlock": "0.0.0.0/0",
		"GatewayId":            igwID,
	}, nil)
}

func (c *Client) AssociateRouteTable(ctx context.Context, routeTableID, subnetID string) error {
	return c.call(ctx, "AssociateRouteTable", map[string]string{
		"RouteTableId": routeTableID,
		"SubnetId":     subnetID,
	}, nil)
}

type subnetItem struct {
	SubnetId                string `xml:"subnetId"`
	VpcId                   string `xml:"vpcId"`
	AvailabilityZone        string `xml:"availabilityZone"`
	CidrBlock               string `xml:"cidrBlock"`
	DefaultForAz            bool   `xml:"defaultForAz"`
	State                   string `xml:"state"`
	AvailableIpAddressCount int    `xml:"availableIpAddressCount"`
	TagSet                  struct {
		Item []struct {
			Key   string `xml:"key"`
			Value string `xml:"value"`
		} `xml:"item"`
	} `xml:"tagSet"`
}

type subnetsResp struct {
	SubnetSet struct {
		Item []subnetItem `xml:"item"`
	} `xml:"subnetSet"`
}

func (c *Client) DescribeSubnets(ctx context.Context, vpcID string) ([]subnetItem, error) {
	var out subnetsResp
	params := map[string]string{
		"Filter.1.Name":    "state",
		"Filter.1.Value.1": "available",
	}
	if vpcID != "" {
		params["Filter.2.Name"] = "vpc-id"
		params["Filter.2.Value.1"] = vpcID
	}
	if err := c.call(ctx, "DescribeSubnets", params, &out); err != nil {
		return nil, err
	}
	return out.SubnetSet.Item, nil
}

type createSubnetResp struct {
	Subnet struct {
		SubnetId         string `xml:"subnetId"`
		AvailabilityZone string `xml:"availabilityZone"`
	} `xml:"subnet"`
}

func (c *Client) CreateSubnet(ctx context.Context, vpcID, zone, cidr string) (string, error) {
	params := map[string]string{
		"VpcId":     vpcID,
		"CidrBlock": cidr,
	}
	if zone != "" {
		params["AvailabilityZone"] = zone
	}
	var out createSubnetResp
	if err := c.call(ctx, "CreateSubnet", params, &out); err != nil {
		return "", err
	}
	return out.Subnet.SubnetId, nil
}

func (c *Client) ListVSwitches(ctx context.Context, vpcID string) ([]cloud.VSwitch, error) {
	items, err := c.DescribeSubnets(ctx, vpcID)
	if err != nil {
		return nil, err
	}
	out := make([]cloud.VSwitch, 0, len(items))
	for _, s := range items {
		out = append(out, cloud.VSwitch{
			ID:    s.SubnetId,
			Name:  "",
			Zone:  s.AvailabilityZone,
			CIDR:  s.CidrBlock,
			VPCID: s.VpcId,
		})
	}
	return out, nil
}

type imagesResp struct {
	ImagesSet struct {
		Item []struct {
			ImageId      string `xml:"imageId"`
			CreationDate string `xml:"creationDate"`
			State        string `xml:"state"`
		} `xml:"item"`
	} `xml:"imagesSet"`
}

func (c *Client) GetImage(ctx context.Context, platform string) (string, error) {
	params := map[string]string{
		"Owner.1": "099720109477",
	}
	if platform == "" {
		platform = "ubuntu"
	}
	patterns := map[string]string{
		"ubuntu": "ubuntu/images/hvm-ssd/ubuntu-*-amd64-server-*",
		"debian": "debian-*-amd64-*",
		"amazon": "amzn2-ami-hvm-*-x86_64-gp2",
	}
	pattern, ok := patterns[platform]
	if !ok {
		pattern = platform
	}
	params["Filter.1.Name"] = "name"
	params["Filter.1.Value.1"] = pattern
	params["Filter.2.Name"] = "state"
	params["Filter.2.Value.1"] = "available"
	params["Filter.3.Name"] = "architecture"
	params["Filter.3.Value.1"] = "x86_64"

	var out imagesResp
	if err := c.call(ctx, "DescribeImages", params, &out); err != nil {
		return "", err
	}
	if len(out.ImagesSet.Item) == 0 {
		return "", fmt.Errorf("no %s image found in %s", platform, c.region)
	}
	items := out.ImagesSet.Item
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].CreationDate > items[j].CreationDate
	})
	return items[0].ImageId, nil
}

func mapStatus(s string) cloud.InstanceStatus {
	switch s {
	case "pending":
		return cloud.StatusPending
	case "running":
		return cloud.StatusRunning
	case "stopping":
		return cloud.StatusStopping
	case "stopped":
		return cloud.StatusStopped
	case "shutting-down":
		return cloud.StatusTerminating
	case "terminated":
		return cloud.StatusTerminated
	default:
		return cloud.StatusUnknown
	}
}

func parseTime(s string) int64 {
	if s == "" {
		return 0
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.999Z07:00",
		"2006-01-02T15:04:05.999999Z",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t.Unix()
		}
	}
	return 0
}

func newNonce() string {
	return fmt.Sprintf("%d%d", time.Now().UnixNano(), int64(time.Now().Nanosecond()%997))
}
