package main

import (
	"flag"
	"strings"

	"github.com/golang/glog"
	"github.com/intel/csi-pmem/pkg/ndctl"
	pmemexec "github.com/intel/csi-pmem/pkg/pmem-exec"
)

func init() {
	flag.Set("logtostderr", "true")
}

func main() {
	flag.Parse()
	ctx, err := ndctl.NewContext()
	if err != nil {
		glog.Fatalf("Failed to initialize pmem context: %s", err.Error())
	}

	prepareVolumeGroups(ctx)
}

// for all regions:
// - Check that VG exists for this region. Create if does not exist
// - For all namespaces in region:
//   - check that PVol exists provided by that namespace, is in current VG, add if does not exist
// Edge cases are when no PVol or VG structures (or partially) dont exist yet
func prepareVolumeGroups(ctx *ndctl.Context) {
	for _, bus := range ctx.GetBuses() {
		glog.Infof("CheckVG: Bus: %v", bus.DeviceName())
		for _, r := range bus.ActiveRegions() {
			glog.Infof("Region: %v", r.DeviceName())
			nsmodes := []string{"fsdax", "sector"}
			for _, nsmod := range nsmodes {
				glog.Infof("NsMode: %v", nsmod)
				vgName := vgName(bus, r, nsmod)
				if err := createVolumesForRegion(r, vgName, nsmod); err != nil {
					glog.Errorf("Failed volumegroup creation: %s", err.Error())
				}
			}
		}
	}
}

func vgName(bus *ndctl.Bus, region *ndctl.Region, nsmode string) string {
	return bus.DeviceName() + region.DeviceName() + nsmode
}

func createVolumesForRegion(r *ndctl.Region, vgName string, nsmode string) error {
	cmd := ""
	cmdArgs := []string{"--force", vgName}
	nsArray := r.ActiveNamespaces()
	if len(nsArray) == 0 {
		glog.Infof("No active namespaces in region %s", r.DeviceName())
		return nil
	}
	for _, ns := range nsArray {
		// consider only namespaces in asked namespacemode
		if ns.Mode() == ndctl.NamespaceMode(nsmode) {
			devName := "/dev/" + ns.BlockDeviceName()
			glog.Infof("createVolumesForRegion: %s has nsmode %s", ns.BlockDeviceName(), nsmode)
			/* check if this pv is already part of a group, if yes ignore this pv
			if not add to arg list */
			output, err := pmemexec.RunCommand("pvs", "--noheadings", "-o", "vg_name", devName)
			if err != nil || len(strings.TrimSpace(output)) == 0 {
				cmdArgs = append(cmdArgs, devName)
			}
		}
	}
	if len(cmdArgs) == 2 {
		glog.Infof("no new namespace found to add to this group: %s", vgName)
		return nil
	}
	if _, err := pmemexec.RunCommand("vgdisplay", vgName); err != nil {
		glog.Infof("No Vgroup with name %v, mark for creation", vgName)
		cmd = "vgcreate"
	} else {
		glog.Infof("VolGroup '%v' exists", vgName)
		cmd = "vgextend"
	}

	_, err := pmemexec.RunCommand(cmd, cmdArgs...) //nolint gosec
	if err != nil {
		return err
	}
	// Tag add works without error if repeated, so it is safe to run without checking for existing
	_, err = pmemexec.RunCommand("vgchange", "--addtag", nsmode, vgName)
	return err
}
