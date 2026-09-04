package modules

import (
	"context"
	"testing"

	remoteexec "github.com/go-remoteexec/transport"
)

func TestModuleHwcEvsDiskCreateSynchronousResponse(t *testing.T) {
	// KooCLI's CreateVolume response has no job_id field at all here —
	// hcloudRunJSONJob treats that as already complete rather than
	// polling, matching every scripted test in this package that has
	// no ShowJob entry to consume.
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud EVS ListVolumes --availability_zone=az1 --name=my-disk --volume_type=SSD": {
			RC: 0, Stdout: `{"volumes":[]}`,
		},
		"hcloud EVS CreateVolume --volume.availability_zone=az1 --volume.name=my-disk --volume.volume_type=SSD": {
			RC: 0, Stdout: `{"entities":{"volume_id":"vol-1"}}`,
		},
	})
	args := map[string]any{"availability_zone": "az1", "name": "my-disk", "volume_type": "SSD"}
	res, err := moduleHwcEvsDisk(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "vol-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcEvsDiskCreatePollsJobToSuccess(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud": {RC: 0},
		"hcloud EVS ListVolumes --availability_zone=az1 --name=my-disk --volume_type=SSD": {
			RC: 0, Stdout: `{"volumes":[]}`,
		},
		"hcloud EVS CreateVolume --volume.availability_zone=az1 --volume.name=my-disk --volume.volume_type=SSD": {
			RC: 0, Stdout: `{"job_id":"job-1"}`,
		},
		"hcloud EVS ShowJob --job_id=job-1": {
			RC: 0, Stdout: `{"job_status":"SUCCESS","entities":{"volume_id":"vol-1"}}`,
		},
	})
	args := map[string]any{"availability_zone": "az1", "name": "my-disk", "volume_type": "SSD"}
	res, err := moduleHwcEvsDisk(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Changed || res.Failed {
		t.Fatalf("res = %+v", res)
	}
	if res.Extra["id"] != "vol-1" {
		t.Fatalf("id = %v", res.Extra["id"])
	}
}

func TestModuleHwcEvsDiskDeleteJobFails(t *testing.T) {
	conn := newFakeConn(map[string]remoteexec.Result{
		"command -v hcloud":                         {RC: 0},
		"hcloud EVS ShowVolume --volume_id=vol-1":   {RC: 0, Stdout: `{"volume":{"id":"vol-1"}}`},
		"hcloud EVS DeleteVolume --volume_id=vol-1": {RC: 0, Stdout: `{"job_id":"job-1"}`},
		"hcloud EVS ShowJob --job_id=job-1": {
			RC: 0, Stdout: `{"job_status":"FAIL","fail_reason":"disk in use"}`,
		},
	})
	args := map[string]any{"availability_zone": "az1", "name": "my-disk", "volume_type": "SSD", "id": "vol-1", "state": "absent"}
	res, err := moduleHwcEvsDisk(context.Background(), conn, args)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Failed {
		t.Fatal("want Failed: job_status=FAIL")
	}
}
