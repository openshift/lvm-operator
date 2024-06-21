package vgmanager

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/go-logr/logr/testr"
	lvmv1alpha1 "github.com/openshift/lvm-operator/v4/api/v1alpha1"
	lvmexec "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec"
	mockExec "github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/exec/test"
	"github.com/openshift/lvm-operator/v4/internal/controllers/vgmanager/lvm"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/strings/slices"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var mockLvsOutputNoReportContent = `
{
      "report": []
}
`

var mockLvsOutputNoLVsInReport = `
{
      "report": [
          {
              "lv": []
          }
      ]
  }
`

var mockLvsOutputWrongLVsInReport = `
{
      "report": [
          {
              "lv": [
                  {"lv_name":"thin-pool-BLUB", "vg_name":"vg1", "lv_attr":"twi-a-tz--", "lv_size":"26.96g", "pool_lv":"", "origin":"", "data_percent":"0.00", "metadata_percent":"10.52", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
              ]
          }
      ]
  }
`

var mockLvsOutputThinPoolValid = `
{
      "report": [
          {
              "lv": [
                  {"lv_name":"thin-pool-1", "vg_name":"vg1", "lv_attr":"twi-a-tz--", "lv_size":"26.96g", "pool_lv":"", "origin":"", "data_percent":"0.00", "metadata_percent":"10.52", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
              ]
          }
      ]
  }
`

var mockLvsOutputThinPoolHighMetadataUse = `
{
      "report": [
          {
              "lv": [
                  {"lv_name":"thin-pool-1", "vg_name":"vg1", "lv_attr":"twi-a-tz--", "lv_size":"26.96g", "pool_lv":"", "origin":"", "data_percent":"0.00", "metadata_percent":"98.52", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
              ]
          }
      ]
  }
`
var mockLvsOutputThinPoolSuspended = `
{
      "report": [
          {
              "lv": [
                  {"lv_name":"thin-pool-1", "vg_name":"vg1", "lv_attr":"twi-s-tz--", "lv_size":"26.96g", "pool_lv":"", "origin":"", "data_percent":"0.00", "metadata_percent":"10.52", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
              ]
          }
      ]
  }
`

var mockLvsOutputRAID = `
{
      "report": [
          {
              "lv": [
                  {"lv_name":"thin-pool-1", "vg_name":"vg1", "lv_attr":"rwi-a-tz--", "lv_size":"26.96g", "pool_lv":"", "origin":"", "data_percent":"0.00", "metadata_percent":"10.52", "move_pv":"", "mirror_log":"", "copy_percent":"", "convert_lv":""}
              ]
          }
      ]
  }
`

func TestVGReconciler_validateLVs(t *testing.T) {
	type fields struct {
		executor lvmexec.Executor
	}
	type args struct {
		volumeGroup *lvmv1alpha1.LVMVolumeGroup
	}

	lvsCommandForVG1 := []string{
		"-S",
		"vgname=vg1",
		"--units",
		"g",
		"--reportformat",
		"json",
	}

	mockExecutorForLVSOutput := func(output string) lvmexec.Executor {
		return &mockExec.MockExecutor{
			MockRunCommandAsHostInto: func(ctx context.Context, into any, command string, args ...string) error {
				if !slices.Equal(args, lvsCommandForVG1) || !strings.Contains(command, "lvs") {
					return fmt.Errorf("invalid args %q", args)
				}
				return json.Unmarshal([]byte(output), &into)
			},
		}
	}

	tests := []struct {
		name    string
		fields  fields
		args    args
		wantErr assert.ErrorAssertionFunc
	}{
		{
			name: "Valid LV",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputThinPoolValid),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.NoError,
		},
		{
			name: "Invalid LV due to Type not being Thin Pool",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputRAID),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.Error,
		},
		{
			name: "Invalid LV due to high metadata percentage",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputThinPoolHighMetadataUse),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.Error,
		},
		{
			name: "Invalid LV due to suspended instead of active state",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputThinPoolSuspended),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.Error,
		},
		{
			name: "Invalid LV due to empty report",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputNoReportContent),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.Error,
		},
		{
			name: "Invalid LV due to no LVs in report",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputNoLVsInReport),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.Error,
		},
		{
			name: "Invalid LV due to wrong LVs in report",
			fields: fields{
				executor: mockExecutorForLVSOutput(mockLvsOutputWrongLVsInReport),
			},
			args: args{volumeGroup: &lvmv1alpha1.LVMVolumeGroup{
				ObjectMeta: metav1.ObjectMeta{Name: "vg1", Namespace: "default"},
				Spec: lvmv1alpha1.LVMVolumeGroupSpec{ThinPoolConfig: &lvmv1alpha1.ThinPoolConfig{
					Name: "thin-pool-1",
				}},
			}},
			wantErr: assert.Error,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := log.IntoContext(context.Background(), testr.New(t))
			r := &Reconciler{LVM: lvm.NewHostLVM(tt.fields.executor)}
			tt.wantErr(t, r.validateLVs(ctx, tt.args.volumeGroup), fmt.Sprintf("validateLVs(%v)", tt.args.volumeGroup))
		})
	}
}
