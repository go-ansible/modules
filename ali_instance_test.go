package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAliInstanceCreate(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceName my-ecs": {
			RC: 0, Stdout: `{"Instances":{"Instance":[]}}`,
		},
		"aliyun ecs CreateInstance --RegionId cn-hangzhou --ImageId img-1 --InstanceType ecs.g6.large --InstanceName my-ecs --SystemDisk.Category cloud_efficiency --SystemDisk.Size 40 --InternetChargeType PayByBandwidth --InternetMaxBandwidthIn 200 --InstanceChargeType PostPaid --SpotStrategy NoSpot": {
			RC: 0, Stdout: `{"InstanceId":"i-newinst"}`,
		},
		"aliyun ecs StartInstance --InstanceId i-newinst": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceIds '[\"i-newinst\"]'": {
			RC: 0, Stdout: `{"Instances":{"Instance":[{"InstanceId":"i-newinst","Status":"Running"}]}}`,
		},
	})
	args := map[string]any{
		"alicloud_region": "cn-hangzhou",
		"instance_name":   "my-ecs",
		"image_id":        "img-1",
		"instance_type":   "ecs.g6.large",
	}
	res, err := moduleAliInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if !res.Changed {
		t.Fatal("want Changed=true")
	}
	ids, ok := res.Extra["ids"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "i-newinst" {
		t.Fatalf("ids = %+v", res.Extra["ids"])
	}
}

func TestModuleAliInstanceCreateSkippedWhenAlreadyPresent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceName my-ecs": {
			RC: 0, Stdout: `{"Instances":{"Instance":[{"InstanceId":"i-existing","Status":"Running"}]}}`,
		},
	})
	args := map[string]any{
		"alicloud_region": "cn-hangzhou",
		"instance_name":   "my-ecs",
		"image_id":        "img-1",
		"instance_type":   "ecs.g6.large",
		"count":           1,
	}
	res, err := moduleAliInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Changed {
		t.Fatal("want Changed=false, instance already exists")
	}
}

func TestModuleAliInstanceStart(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceIds '[\"i-abc\"]'": {
			RC: 0, Stdout: `{"Instances":{"Instance":[{"InstanceId":"i-abc","Status":"Stopped"}]}}`,
		},
		"aliyun ecs StartInstance --InstanceId i-abc": {RC: 0},
	})
	args := map[string]any{
		"alicloud_region": "cn-hangzhou",
		"instance_ids":    []any{"i-abc"},
		"state":           "running",
	}
	res, err := moduleAliInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAliInstanceStartAlreadyRunningNoop(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceIds '[\"i-abc\"]'": {
			RC: 0, Stdout: `{"Instances":{"Instance":[{"InstanceId":"i-abc","Status":"Running"}]}}`,
		},
	})
	args := map[string]any{
		"alicloud_region": "cn-hangzhou",
		"instance_ids":    []any{"i-abc"},
		"state":           "running",
	}
	res, err := moduleAliInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAliInstanceAbsent(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceIds '[\"i-abc\"]'": {
			RC: 0, Stdout: `{"Instances":{"Instance":[{"InstanceId":"i-abc","Status":"Running"}]}}`,
		},
		"aliyun ecs DeleteInstance --InstanceId i-abc --Force false": {RC: 0},
	})
	args := map[string]any{
		"alicloud_region": "cn-hangzhou",
		"instance_ids":    []any{"i-abc"},
		"state":           "absent",
	}
	res, err := moduleAliInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed || !res.Changed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAliInstanceNotFoundFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceIds '[\"i-gone\"]'": {
			RC: 0, Stdout: `{"Instances":{"Instance":[]}}`,
		},
	})
	args := map[string]any{
		"alicloud_region": "cn-hangzhou",
		"instance_ids":    []any{"i-gone"},
		"state":           "absent",
	}
	res, err := moduleAliInstance(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatalf("want Failed, res = %+v", res)
	}
}

func TestModuleAliInstanceMissingRegion(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAliInstance(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing alicloud_region")
	}
}

func TestModuleAliInstanceRequiresInstanceIDsForNonPresent(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAliInstance(context.Background(), conn, map[string]any{
		"alicloud_region": "cn-hangzhou",
		"state":           "stopped",
	})
	if err == nil {
		t.Fatal("want error: instance_ids required for state=stopped")
	}
}
