package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleAliInstanceInfoBasic(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou": {
			RC: 0, Stdout: `{"Instances":{"Instance":[{"InstanceId":"i-abc","Status":"Running"}]}}`,
		},
	})
	res, err := moduleAliInstanceInfo(context.Background(), conn, map[string]any{"alicloud_region": "cn-hangzhou"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
	ids, ok := res.Extra["ids"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "i-abc" {
		t.Fatalf("ids = %+v", res.Extra["ids"])
	}
}

func TestModuleAliInstanceInfoNamePrefix(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v aliyun": {RC: 0},
		"aliyun ecs DescribeInstances --RegionId cn-hangzhou --InstanceName ecs_instance_*": {
			RC: 0, Stdout: `{"Instances":{"Instance":[]}}`,
		},
	})
	res, err := moduleAliInstanceInfo(context.Background(), conn, map[string]any{
		"alicloud_region": "cn-hangzhou",
		"name_prefix":     "ecs_instance_",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Failed {
		t.Fatalf("res = %+v", res)
	}
}

func TestModuleAliInstanceInfoMissingRegion(t *testing.T) {
	conn := newFakeConn(nil)
	_, err := moduleAliInstanceInfo(context.Background(), conn, map[string]any{})
	if err == nil {
		t.Fatal("want error for missing alicloud_region")
	}
}

func TestAliFilterParamName(t *testing.T) {
	cases := map[string]string{
		"instance_ids": "InstanceIds",
		"vpc-id":       "VpcId",
		"InstanceIds":  "InstanceIds",
	}
	for in, want := range cases {
		if got := aliFilterParamName(in); got != want {
			t.Errorf("aliFilterParamName(%q) = %q, want %q", in, got, want)
		}
	}
}
