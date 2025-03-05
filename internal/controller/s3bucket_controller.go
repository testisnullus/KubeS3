/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"

	awsv1 "github.com/testisnullus/KubeS3/api/v1"
	"github.com/testisnullus/KubeS3/internal/models"
	"github.com/testisnullus/KubeS3/internal/ratelimiter"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/credentials"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/s3"
	"github.com/go-logr/logr"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// S3BucketReconciler reconciles a S3Bucket object
type S3BucketReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=aws.nullzen.ai,resources=s3buckets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=aws.nullzen.ai,resources=s3buckets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=aws.nullzen.ai,resources=s3buckets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the S3Bucket object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.19.4/pkg/reconcile
func (r *S3BucketReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	l := log.FromContext(ctx)

	b := &awsv1.S3Bucket{}
	err := r.Client.Get(ctx, req.NamespacedName, b)
	if err != nil {
		if errors.IsNotFound(err) {
			l.Info("S3 bucket resource is not found",
				"request", req)
			return ctrl.Result{}, nil
		}

		l.Error(err, "Unable to fetch S3 bucket",
			"request", req)
		return ctrl.Result{}, err
	}

	switch b.Annotations[models.StateAnnotation] {
	case models.CreatingEvent:
		l = l.WithName("S3 Bucket creation")
		return r.handleCreate(ctx, &l, b)
	// TODO: add update event handling
	case models.DeletingEvent:
		l = l.WithName("S3 Bucket deleting")
		return r.handleDelete(ctx, &l, b)
	case models.GenericEvent:
		l = l.WithName("S3 Bucket generic")
		l.Info("Event isn't handled",
			"request", req,
			"bucket", b.Spec.BucketName,
			"event", b.Annotations[models.StateAnnotation])
		return ctrl.Result{}, nil
	}

	return ctrl.Result{}, nil
}

func (r *S3BucketReconciler) handleCreate(ctx context.Context, l *logr.Logger, b *awsv1.S3Bucket) (ctrl.Result, error) {
	l.Info(
		"Creating bucket",
		"bucket name", b.Spec.BucketName,
		"region", b.Spec.Region,
	)

	s, err := r.handleSecret(ctx, b)
	if err != nil {
		return ctrl.Result{}, err
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(b.Spec.Region),
		Credentials: credentials.NewStaticCredentialsFromCreds(credentials.Value{
			AccessKeyID:     string(s.Data["aws_access_key_id"]),
			SecretAccessKey: string(s.Data["aws_secret_access_key"]),
		}),
	},
	)

	if err != nil {
		l.Error(err, "Unable to create AWS session", "spec", b.Spec)
		return ctrl.Result{}, err
	}

	// Create S3 service client
	svc := s3.New(sess)
	_, err = svc.CreateBucket(&s3.CreateBucketInput{
		Bucket: aws.String(b.Spec.BucketName),
	})
	if err != nil {
		l.Error(err, "Unable to create S3 Bucket", "bucket", b.Spec)
		return ctrl.Result{}, err
	}

	// Wait until bucket is created before finishing
	l.Info("Waiting for bucket to be created...", "spec", b.Spec)

	err = svc.WaitUntilBucketExists(&s3.HeadBucketInput{
		Bucket: aws.String(b.Spec.BucketName),
	})
	if err != nil {
		l.Error(err, "Unable to wait for bucket to be created", "spec", b.Spec)
		return ctrl.Result{}, err
	}

	patch := b.NewPatch()
	controllerutil.AddFinalizer(b, models.DeletionFinalizer)
	b.Annotations[models.StateAnnotation] = models.CreatedEvent
	err = r.Patch(ctx, b, patch)
	if err != nil {
		l.Error(err, "Unable to patch S3 bucket with deletion finalizer", "spec", b.Spec)
		return ctrl.Result{}, err
	}

	l.Info("Bucket successfully created", "spec", b.Spec)

	return ctrl.Result{}, nil
}

func (r *S3BucketReconciler) handleSecret(ctx context.Context, b *awsv1.S3Bucket) (*v1.Secret, error) {
	secret := &v1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: b.Spec.AWSCredsSecretRef.Name, Namespace: b.Spec.AWSCredsSecretRef.Namespace}, secret)
	if err != nil {
		return nil, err
	}

	return secret, nil
}

func (r *S3BucketReconciler) handleDelete(ctx context.Context, l *logr.Logger, b *awsv1.S3Bucket) (ctrl.Result, error) {
	l.Info(
		"Deleting bucket",
		"bucket name", b.Spec.BucketName,
		"region", b.Spec.Region,
	)

	s, err := r.handleSecret(ctx, b)
	if err != nil {
		return ctrl.Result{}, err
	}

	sess, err := session.NewSession(&aws.Config{
		Region: aws.String(b.Spec.Region),
		Credentials: credentials.NewStaticCredentialsFromCreds(credentials.Value{
			AccessKeyID:     string(s.Data["aws_access_key_id"]),
			SecretAccessKey: string(s.Data["aws_secret_access_key"]),
		}),
	},
	)

	if err != nil {
		l.Error(err, "Unable to create AWS session", "spec", b.Spec)
		return ctrl.Result{}, err
	}

	// Create S3 service client
	svc := s3.New(sess)
	_, err = svc.DeleteBucket(&s3.DeleteBucketInput{
		Bucket: aws.String(b.Spec.BucketName),
	})
	if err != nil {
		l.Error(err, "Unable to delete S3 Bucket", "bucket", b.Spec)
		return ctrl.Result{}, err
	}

	// Wait until bucket is created before finishing
	l.Info("Waiting for bucket to be deleted...", "spec", b.Spec)

	err = svc.WaitUntilBucketNotExists(&s3.HeadBucketInput{
		Bucket: aws.String(b.Spec.BucketName),
	})
	if err != nil {
		l.Error(err, "Unable to wait for bucket to be deleted", "spec", b.Spec)
		return ctrl.Result{}, err
	}

	patch := b.NewPatch()
	controllerutil.RemoveFinalizer(b, models.DeletionFinalizer)
	b.Annotations[models.StateAnnotation] = models.DeletedEvent
	err = r.Patch(ctx, b, patch)
	if err != nil {
		l.Error(err, "Unable to remove S3 Bucket finalizer", "spec", b.Spec)
		return ctrl.Result{}, err
	}

	l.Info("Bucket successfully deleted", "spec", b.Spec)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *S3BucketReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&awsv1.S3Bucket{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(event event.CreateEvent) bool {
				if event.Object.GetDeletionTimestamp() != nil {
					event.Object.GetAnnotations()[models.StateAnnotation] = models.DeletingEvent
					return true
				}

				event.Object.GetAnnotations()[models.StateAnnotation] = models.CreatingEvent
				return true
			},
			UpdateFunc: func(event event.UpdateEvent) bool {
				newObj := event.ObjectNew.(*awsv1.S3Bucket)
				if newObj.Generation == event.ObjectOld.GetGeneration() {
					return false
				}

				if newObj.DeletionTimestamp != nil {
					event.ObjectNew.GetAnnotations()[models.StateAnnotation] = models.DeletingEvent
					return true
				}

				newObj.Annotations[models.StateAnnotation] = models.UpdatingEvent
				return true
			},
			GenericFunc: func(genericEvent event.GenericEvent) bool {
				genericEvent.Object.GetAnnotations()[models.StateAnnotation] = models.GenericEvent
				return true
			},
		})).
		WithOptions(controller.Options{
			RateLimiter: ratelimiter.NewItemExponentialFailureRateLimiterWithMaxTries(ratelimiter.DefaultBaseDelay, ratelimiter.DefaultMaxDelay),
		}).
		Named("s3bucket").
		Complete(r)
}
