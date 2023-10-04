package labels

import (
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// orients itself on https://sdk.operatorframework.io/docs/building-operators/ansible/reference/retroactively-owned-resources/#for-objects-which-are-not-in-the-same-namespace-as-the-owner-cr
// but uses labels and split apiVersion,kind and namespace,name to make label searches and filters possible.
const (
	OwnedByPrefix    = "owned-by.topolvm.io"
	OwnedByName      = OwnedByPrefix + "/name"
	OwnedByNamespace = OwnedByPrefix + "/namespace"
	OwnedByUID       = OwnedByPrefix + "/uid"
	OwnedByGroup     = OwnedByPrefix + "/group"
	OwnedByVersion   = OwnedByPrefix + "/version"
	OwnedByKind      = OwnedByPrefix + "/kind"
)

func SetManagedLabels(scheme *runtime.Scheme, obj client.Object, owner client.Object) {
	lbls := obj.GetLabels()
	if lbls == nil {
		lbls = make(map[string]string)
	}
	lbls[OwnedByName] = owner.GetName()
	lbls[OwnedByNamespace] = owner.GetNamespace()
	lbls[OwnedByUID] = string(owner.GetUID())
	if runtimeObj, ok := owner.(runtime.Object); ok {
		if gvk, err := apiutil.GVKForObject(runtimeObj, scheme); err == nil {
			lbls[OwnedByGroup] = gvk.Group
			lbls[OwnedByVersion] = gvk.Version
			lbls[OwnedByKind] = gvk.Kind
		}
	}
	obj.SetLabels(lbls)
}

func MatchesManagedLabels(scheme *runtime.Scheme, obj client.Object, owner client.Object) bool {
	if lbls := obj.GetLabels(); lbls != nil {
		baseMatch := lbls[OwnedByName] == owner.GetName() &&
			lbls[OwnedByNamespace] == owner.GetNamespace() &&
			lbls[OwnedByUID] == string(owner.GetUID())

		if !baseMatch {
			return false
		}

		if ownerRuntimeObj, ok := owner.(runtime.Object); ok {
			if gvk, err := apiutil.GVKForObject(ownerRuntimeObj, scheme); err == nil {
				return lbls[OwnedByGroup] == gvk.Group &&
					lbls[OwnedByVersion] == gvk.Version &&
					lbls[OwnedByKind] == gvk.Kind
			}
		}
	}
	return false
}
