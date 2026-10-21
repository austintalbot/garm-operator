// SPDX-License-Identifier: MIT

package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cloudbase/garm/client/organizations"
	"github.com/cloudbase/garm/client/scalesets"
	"github.com/cloudbase/garm/params"
	"go.uber.org/mock/gomock"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	garmoperatorv1beta1 "github.com/mercedes-benz/garm-operator/api/v1beta1"
	"github.com/mercedes-benz/garm-operator/pkg/client/key"
	"github.com/mercedes-benz/garm-operator/pkg/client/mock"
	"github.com/mercedes-benz/garm-operator/pkg/conditions"
)

func scheduledScaleSet() *garmoperatorv1beta1.ScaleSet {
	set := newTestScaleSet("scheduled")
	set.Spec.MinIdleRunners = 0
	set.Spec.RunnerSchedule = &garmoperatorv1beta1.RunnerSchedule{TimeZone: "UTC", Ranges: []garmoperatorv1beta1.RunnerScheduleRange{{
		Days: []string{"Monday"}, Start: "08:00", End: "18:00", MinIdleRunners: 5,
	}}}
	return set
}

func scheduledLive(set *garmoperatorv1beta1.ScaleSet, minimum uint) params.ScaleSet {
	return params.ScaleSet{
		ID: 42, Name: set.Name, OrgID: testScaleSetOrgID,
		ProviderName: set.Spec.ProviderName, Image: set.Spec.Image, Flavor: set.Spec.Flavor,
		OSType: set.Spec.OSType, OSArch: set.Spec.OSArch, MaxRunners: set.Spec.MaxRunners,
		MinIdleRunners: minimum, RunnerBootstrapTimeout: set.Spec.RunnerBootstrapTimeout,
		Enabled: set.Spec.Enabled, Tags: []params.Tag{{Name: "large"}, {Name: "ubuntu"}},
	}
}

func scheduleRequest(set *garmoperatorv1beta1.ScaleSet) reconcile.Request {
	return reconcile.Request{NamespacedName: types.NamespacedName{Namespace: set.Namespace, Name: set.Name}}
}

func scheduleReconciler(t *testing.T, set *garmoperatorv1beta1.ScaleSet, remote *mock.MockScaleSetClient) *ScaleSetReconciler {
	t.Helper()
	return &ScaleSetReconciler{
		Client:   fake.NewClientBuilder().WithScheme(scaleSetTestScheme(t)).WithObjects(set, newTestOrg()).WithStatusSubresource(set).Build(),
		Recorder: events.NewFakeRecorder(20), ScaleSetClient: remote,
		InstanceClient: mock.NewMockInstanceClient(gomock.NewController(t)),
		Now:            func() time.Time { return time.Date(2026, 10, 19, 12, 0, 30, 0, time.UTC) },
	}
}

func setupNormalPathMocks(t *testing.T, path string, set *garmoperatorv1beta1.ScaleSet, remote *mock.MockScaleSetClient) {
	t.Helper()
	live := scheduledLive(set, 0)
	if path == "matching" {
		live.MinIdleRunners = 5
	}
	if path == "recreate" {
		live.ProviderName = "old-provider"
		live.Enabled = false
	}
	if path == "stale create" || path == "stale adopt" {
		remote.EXPECT().GetScaleSet(uint(42)).Return(nil, scalesets.NewGetScaleSetDefault(404))
	} else if set.Status.ID != "" {
		remote.EXPECT().GetScaleSet(uint(42)).Return(&scalesets.GetScaleSetOK{Payload: live}, nil)
	}
	if path == "create" || path == "stale create" {
		remote.EXPECT().ListOrgScaleSets(testScaleSetOrgID).Return(&organizations.ListOrgScaleSetsOK{}, nil)
	}
	if path == "adopt" || path == "stale adopt" {
		remote.EXPECT().ListOrgScaleSets(testScaleSetOrgID).Return(&organizations.ListOrgScaleSetsOK{Payload: []params.ScaleSet{live}}, nil)
	}
	if path == "recreate" {
		remote.EXPECT().DeleteScaleSet(uint(42)).Return(nil)
	}
	if path == "create" || path == "stale create" || path == "recreate" {
		remote.EXPECT().CreateOrgScaleSet(testScaleSetOrgID, gomock.Any()).DoAndReturn(func(_ string, p *organizations.CreateOrgScaleSetParams) (*organizations.CreateOrgScaleSetOK, error) {
			if p.Body.MinIdleRunners != 5 {
				t.Fatalf("create minimum = %d, want 5", p.Body.MinIdleRunners)
			}
			return &organizations.CreateOrgScaleSetOK{Payload: params.ScaleSet{ID: 42, Enabled: true}}, nil
		})
	} else if path != "matching" {
		remote.EXPECT().UpdateScaleSet(uint(42), gomock.Any()).DoAndReturn(func(_ uint, p *scalesets.UpdateScaleSetParams) (*scalesets.UpdateScaleSetOK, error) {
			if p.Body.MinIdleRunners == nil || *p.Body.MinIdleRunners != 5 {
				t.Fatalf("update minimum = %v, want 5", p.Body.MinIdleRunners)
			}
			if p.Body.Enabled == nil || *p.Body.Enabled != set.Spec.Enabled {
				t.Fatalf("update enabled = %v, want %v", p.Body.Enabled, set.Spec.Enabled)
			}
			return &scalesets.UpdateScaleSetOK{Payload: scheduledLive(set, 5)}, nil
		})
	}
}

func TestScaleSetScheduleNormalPaths(t *testing.T) {
	for _, path := range []string{"create", "update", "matching", "adopt", "stale create", "stale adopt", "recreate", "disabled"} {
		t.Run(path, func(t *testing.T) {
			set := scheduledScaleSet()
			if path != "create" && path != "adopt" {
				set.Status.ID = "42"
			}
			if path == "adopt" || path == "stale adopt" {
				set.Annotations = map[string]string{key.AdoptAnnotation: "true"}
			}
			if path == "disabled" {
				set.Spec.Enabled = false
			}
			remote := mock.NewMockScaleSetClient(gomock.NewController(t))
			setupNormalPathMocks(t, path, set, remote)

			r := scheduleReconciler(t, set, remote)
			res, err := r.Reconcile(context.Background(), scheduleRequest(set))
			if err != nil {
				t.Fatal(err)
			}
			// Monday 12:00:30 UTC -> next transition is Monday 18:00:00 UTC (5h 59m 30s)
			wantDelay := (5*time.Hour + 59*time.Minute + 30*time.Second)
			if res.RequeueAfter != wantDelay {
				t.Fatalf("requeue = %v, want %v", res.RequeueAfter, wantDelay)
			}
			got := &garmoperatorv1beta1.ScaleSet{}
			if err := r.Get(context.Background(), scheduleRequest(set).NamespacedName, got); err != nil {
				t.Fatal(err)
			}
			if got.Spec.MinIdleRunners != 0 {
				t.Fatalf("baseline mutated to %d", got.Spec.MinIdleRunners)
			}
			if got.Status.EffectiveMinIdleRunners == nil || *got.Status.EffectiveMinIdleRunners != 5 {
				t.Fatalf("effectiveMinIdleRunners = %v, want 5", got.Status.EffectiveMinIdleRunners)
			}
		})
	}
}

func TestScaleSetScheduleDynamicRequeue(t *testing.T) {
	ctx := context.Background()
	set := scheduledScaleSet()
	set.Status.ID = "42"
	set.Spec.MinIdleRunners = 2

	remote := mock.NewMockScaleSetClient(gomock.NewController(t))
	r := scheduleReconciler(t, set, remote)

	live := scheduledLive(set, 2)
	remote.EXPECT().GetScaleSet(uint(42)).Return(&scalesets.GetScaleSetOK{Payload: live}, nil)
	remote.EXPECT().UpdateScaleSet(uint(42), gomock.Any()).DoAndReturn(func(_ uint, p *scalesets.UpdateScaleSetParams) (*scalesets.UpdateScaleSetOK, error) {
		if p.Body.MinIdleRunners == nil || *p.Body.MinIdleRunners != 5 {
			t.Fatalf("minimum = %v, want 5", p.Body.MinIdleRunners)
		}
		live.MinIdleRunners = 5
		return &scalesets.UpdateScaleSetOK{Payload: live}, nil
	})

	// Time is Monday 12:00:30 UTC. Schedule range is Mon 08:00 - 18:00. Next transition is at 18:00:00 UTC.
	r.Now = func() time.Time { return time.Date(2026, 10, 19, 12, 0, 30, 0, time.UTC) }

	res, err := r.Reconcile(ctx, scheduleRequest(set))
	if err != nil {
		t.Fatal(err)
	}

	wantDelay := 5*time.Hour + 59*time.Minute + 30*time.Second
	if res.RequeueAfter != wantDelay {
		t.Fatalf("requeue = %v, want %v", res.RequeueAfter, wantDelay)
	}

	got := &garmoperatorv1beta1.ScaleSet{}
	if err := r.Get(ctx, scheduleRequest(set).NamespacedName, got); err != nil {
		t.Fatal(err)
	}
	if got.Spec.MinIdleRunners != 2 {
		t.Fatalf("baseline mutated to %d", got.Spec.MinIdleRunners)
	}
	if got.Status.EffectiveMinIdleRunners == nil || *got.Status.EffectiveMinIdleRunners != 5 {
		t.Fatalf("effectiveMinIdleRunners = %v, want 5", got.Status.EffectiveMinIdleRunners)
	}
}

func TestScaleSetScheduleLifecycle(t *testing.T) {
	const (
		invalidPath     = "invalid"
		statusErrorPath = "status error"
	)
	for _, path := range []string{"paused", "retained", invalidPath, "remote error", statusErrorPath, "drain"} {
		t.Run(path, func(t *testing.T) {
			set := scheduledScaleSet()
			set.Status.ID = "42"
			remote := mock.NewMockScaleSetClient(gomock.NewController(t))
			switch path {
			case "paused":
				set.Annotations = map[string]string{key.PausedAnnotation: "true"}
			case "retained":
				now := metav1.Now()
				set.DeletionTimestamp = &now
				set.Spec.DeletionPolicy = garmoperatorv1beta1.ScaleSetDeletionPolicyRetain
			case invalidPath:
				set.Spec.RunnerSchedule.TimeZone = "bad-zone"
			case "remote error":
				remote.EXPECT().GetScaleSet(uint(42)).Return(nil, errors.New("remote unavailable"))
			case statusErrorPath:
				remote.EXPECT().GetScaleSet(uint(42)).Return(&scalesets.GetScaleSetOK{Payload: scheduledLive(set, 5)}, nil)
			case "drain":
				live := scheduledLive(set, 0)
				live.ProviderName = "old-provider"
				live.Enabled = false
				live.Instances = []params.Instance{{Name: "busy", RunnerStatus: params.RunnerActive}}
				remote.EXPECT().GetScaleSet(uint(42)).Return(&scalesets.GetScaleSetOK{Payload: live}, nil)
			}
			r := scheduleReconciler(t, set, remote)
			if path == statusErrorPath {
				r.Client = interceptor.NewClient(r.Client.(client.WithWatch), interceptor.Funcs{SubResourceUpdate: func(context.Context, client.Client, string, client.Object, ...client.SubResourceUpdateOption) error {
					return errors.New("status unavailable")
				}})
			}
			res, err := r.Reconcile(context.Background(), scheduleRequest(set))
			// Invalid spec sets condition and returns nil error to prevent backoff loops
			wantError := path == "remote error" || path == statusErrorPath
			if (err != nil) != wantError {
				t.Fatalf("error = %v, wantError %v", err, wantError)
			}
			switch path {
			case "drain":
				if res.RequeueAfter <= 0 || res.RequeueAfter > time.Minute {
					t.Fatalf("lost drain requeue: %v", res)
				}
			case invalidPath, "paused", "retained":
				if res.RequeueAfter != 0 {
					t.Fatalf("unexpected schedule requeue: %v", res)
				}
			}
			if path == invalidPath {
				got := &garmoperatorv1beta1.ScaleSet{}
				if err := r.Get(context.Background(), scheduleRequest(set).NamespacedName, got); err != nil {
					t.Fatal(err)
				}
				if c := conditions.Get(got, conditions.ReadyCondition); c == nil || c.Status != metav1.ConditionFalse || c.Reason != string(conditions.InvalidSpecReason) {
					t.Fatalf("invalid schedule not marked unready with InvalidSpec: %#v", c)
				}
			}
		})
	}
}

func TestScaleSetScheduleDeleteInvalidTimezone(t *testing.T) {
	for _, policy := range []garmoperatorv1beta1.ScaleSetDeletionPolicy{garmoperatorv1beta1.ScaleSetDeletionPolicyDelete, garmoperatorv1beta1.ScaleSetDeletionPolicyRetain} {
		t.Run(string(policy), func(t *testing.T) {
			set := scheduledScaleSet()
			set.Status.ID = "42"
			set.Spec.RunnerSchedule.TimeZone = "Mars/Olympus"
			set.Spec.DeletionPolicy = policy
			now := metav1.Now()
			set.DeletionTimestamp = &now
			remote := mock.NewMockScaleSetClient(gomock.NewController(t))
			if policy == garmoperatorv1beta1.ScaleSetDeletionPolicyDelete {
				live := scheduledLive(set, 5)
				live.Enabled = false
				remote.EXPECT().GetScaleSet(uint(42)).Return(&scalesets.GetScaleSetOK{Payload: live}, nil)
				remote.EXPECT().DeleteScaleSet(uint(42)).Return(nil)
			}
			r := scheduleReconciler(t, set, remote)
			if _, err := r.Reconcile(context.Background(), scheduleRequest(set)); err != nil {
				t.Fatal(err)
			}
			got := &garmoperatorv1beta1.ScaleSet{}
			err := r.Get(context.Background(), scheduleRequest(set).NamespacedName, got)
			if err != nil && !apierrors.IsNotFound(err) {
				t.Fatal(err)
			}
			for _, f := range got.Finalizers {
				if f == key.ScaleSetFinalizerName {
					t.Fatal("invalid timezone blocked finalizer removal")
				}
			}
		})
	}
}
