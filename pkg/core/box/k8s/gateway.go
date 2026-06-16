//go:build k8s

package k8s

import (
	"context"
	"encoding/base64"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// pipeGVR is the sshpiper Kubernetes plugin's CRD. The design note
// (docs/K8S-AGENT-BOX-RUNTIME-DESIGN.md) calls it "PiperUpstream"; the
// maintained plugin's actual resource is `pipes` in group sshpiper.com/v1beta1
// (kind Pipe). sshpiper watches a namespace for these and routes incoming SSH
// by username — so programming a Pipe is how the gateway learns to forward a
// tenant's connection to its box pod.
var pipeGVR = schema.GroupVersionResource{Group: "sshpiper.com", Version: "v1beta1", Resource: "pipes"}

func pipeName(tenant string) string { return "box-" + tenant }

// gatewayEnabled reports whether SSH-gateway routing should be programmed: a
// dynamic client plus a configured gateway namespace where Pipes live. When
// off (no GatewayNamespace), boxes are still reconciled but not routed — useful
// for clusters without sshpiper, and for the core lifecycle e2e.
func (b *Backend) gatewayEnabled() bool {
	return b.dyn != nil && b.cfg.GatewayNamespace != ""
}

// upstreamHost is the in-cluster DNS the gateway forwards the tenant's SSH to:
// the headless Service's stable per-pod name (box-0.boxes.<ns>.svc).
func (b *Backend) upstreamHost(tenant string) string {
	return fmt.Sprintf("%s-0.%s.%s.svc.cluster.local:%d", statefulSetName, serviceName, b.namespaceFor(tenant), sshPort)
}

// pipeObject builds the sshpiper Pipe that routes username=<tenant> to the
// tenant's box pod: the incoming connection authenticates against the box's
// authorized keys (inline, base64), and the upstream host key is trusted
// (ignore_hostkey — TOFU/known_hosts pinning is a follow-up).
func (b *Backend) pipeObject(tenant string, keys []string) *unstructured.Unstructured {
	var buf []byte
	for _, k := range keys {
		buf = append(buf, []byte(k)...)
		buf = append(buf, '\n')
	}
	return &unstructured.Unstructured{Object: map[string]any{
		"apiVersion": "sshpiper.com/v1beta1",
		"kind":       "Pipe",
		"metadata": map[string]any{
			"name":      pipeName(tenant),
			"namespace": b.cfg.GatewayNamespace,
			"labels":    toAnyMap(boxLabels(tenant)),
		},
		"spec": map[string]any{
			"from": []any{map[string]any{
				"username":             tenant,
				"authorized_keys_data": base64.StdEncoding.EncodeToString(buf),
			}},
			"to": map[string]any{
				"host":           b.upstreamHost(tenant),
				"username":       tenant,
				"ignore_hostkey": true,
			},
		},
	}}
}

// upsertPipe creates or updates the tenant's Pipe in the gateway namespace.
// No-op when the gateway isn't configured.
func (b *Backend) upsertPipe(ctx context.Context, tenant string, keys []string) error {
	if !b.gatewayEnabled() {
		return nil
	}
	ns := b.cfg.GatewayNamespace
	obj := b.pipeObject(tenant, keys)
	_, err := b.dyn.Resource(pipeGVR).Namespace(ns).Create(ctx, obj, metav1.CreateOptions{})
	if apierrors.IsAlreadyExists(err) {
		existing, gerr := b.dyn.Resource(pipeGVR).Namespace(ns).Get(ctx, pipeName(tenant), metav1.GetOptions{})
		if gerr != nil {
			return gerr
		}
		existing.Object["spec"] = obj.Object["spec"]
		_, err = b.dyn.Resource(pipeGVR).Namespace(ns).Update(ctx, existing, metav1.UpdateOptions{})
	}
	return err
}

// deletePipe removes the tenant's Pipe. No-op when the gateway isn't configured
// or the Pipe is already gone. (The Pipe lives in the gateway namespace, so the
// per-tenant namespace delete does not cascade to it — it must be deleted
// explicitly.)
func (b *Backend) deletePipe(ctx context.Context, tenant string) error {
	if !b.gatewayEnabled() {
		return nil
	}
	err := b.dyn.Resource(pipeGVR).Namespace(b.cfg.GatewayNamespace).Delete(ctx, pipeName(tenant), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func toAnyMap(m map[string]string) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
