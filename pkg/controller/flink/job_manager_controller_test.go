package flink

import (
	"testing"

	k8mock "github.com/lyft/flinkk8soperator/pkg/controller/k8/mock"
	mockScope "github.com/lyft/flytestdlib/promutils"

	"context"

	"github.com/lyft/flinkk8soperator/pkg/controller/common"
	"github.com/lyft/flytestdlib/promutils/labeled"
	"github.com/operator-framework/operator-sdk/pkg/sdk"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/apps/v1"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/api/extensions/v1beta1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func getJMControllerForTest() JobManagerController {
	testScope := mockScope.NewTestScope()
	labeled.SetMetricKeys(common.GetValidLabelNames()...)

	return JobManagerController{
		metrics:   newJobManagerMetrics(testScope),
		k8Cluster: &k8mock.K8Cluster{},
	}
}

func TestGetJobManagerName(t *testing.T) {
	app := getFlinkTestApp()
	assert.Equal(t, "app-name-"+testAppHash+"-jm", getJobManagerName(&app, testAppHash))
}

func TestGetJobManagerPodName(t *testing.T) {
	app := getFlinkTestApp()
	assert.Equal(t, "app-name-"+testAppHash+"-jm-pod", getJobManagerPodName(&app, testAppHash))
}

func TestGetJobManagerDeployment(t *testing.T) {
	app := getFlinkTestApp()
	deployment := v1.Deployment{}
	deployment.Name = getJobManagerName(&app, testAppHash)
	deployments := []v1.Deployment{
		deployment,
	}
	assert.Equal(t, deployment, *getJobManagerDeployment(deployments, &app))
}

func TestGetJobManagerReplicaCount(t *testing.T) {
	app := getFlinkTestApp()
	deployment := v1.Deployment{}
	deployment.Name = getJobManagerName(&app, HashForApplication(&app))
	replicaCount := int32(2)
	deployment.Spec.Replicas = &replicaCount
	deployments := []v1.Deployment{
		deployment,
	}
	assert.Equal(t, int32(2), getJobManagerCount(deployments, &app))
}

func TestJobManagerCreateSuccess(t *testing.T) {
	testController := getJMControllerForTest()
	app := getFlinkTestApp()
	annotations := map[string]string{
		"key":                   "annotation",
		"flink-app-parallelism": "8",
	}
	app.Annotations = annotations
	hash := "2c193a3b"
	expectedLabels := map[string]string{
		"app":                   "app-name",
		"flink-app-hash":        hash,
		"flink-deployment-type": "jobmanager",
	}
	ctr := 0
	mockK8Cluster := testController.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.CreateK8ObjectFunc = func(ctx context.Context, object sdk.Object) error {
		ctr++
		switch ctr {
		case 1:
			deployment := object.(*v1.Deployment)
			assert.Equal(t, getJobManagerName(&app, hash), deployment.Name)
			assert.Equal(t, app.Namespace, deployment.Namespace)
			assert.Equal(t, getJobManagerPodName(&app, hash), deployment.Spec.Template.Name)
			assert.Equal(t, annotations, deployment.Annotations)
			assert.Equal(t, annotations, deployment.Spec.Template.Annotations)
			assert.Equal(t, app.Namespace, deployment.Spec.Template.Namespace)
			assert.Equal(t, expectedLabels, deployment.Labels)
			assert.Equal(t, int32(1), *deployment.Spec.Replicas)

			assert.Equal(t, "blob.server.port: 6125\njobmanager.heap.size: 1536\n"+
				"jobmanager.rpc.address: app-name-jm\njobmanager.rpc.port: 6123\n"+
				"jobmanager.web.port: 8081\nmetrics.internal.query-service.port: 50101\n"+
				"query.server.port: 6124\ntaskmanager.heap.size: 512\n"+
				"taskmanager.numberOfTaskSlots: 16\n\n"+
				"high-availability.cluster-id: app-name-"+hash+"\n",
				common.GetEnvVar(deployment.Spec.Template.Spec.Containers[0].Env,
					"OPERATOR_FLINK_CONFIG").Value)
		case 2:
			service := object.(*coreV1.Service)
			assert.Equal(t, getJobManagerServiceName(&app), service.Name)
			assert.Equal(t, app.Namespace, service.Namespace)
			assert.Equal(t, map[string]string{"frontend": "app-name-jm"}, service.Spec.Selector)
		case 3:
			labels := map[string]string{
				"app": "app-name",
			}
			ingress := object.(*v1beta1.Ingress)
			assert.Equal(t, app.Name, ingress.Name)
			assert.Equal(t, app.Namespace, ingress.Namespace)
			assert.Equal(t, labels, ingress.Labels)
		}
		return nil
	}
	err := testController.CreateIfNotExist(context.Background(), &app)
	assert.Nil(t, err)
}

func TestJobManagerCreateErr(t *testing.T) {
	testController := getJMControllerForTest()
	app := getFlinkTestApp()
	mockK8Cluster := testController.k8Cluster.(*k8mock.K8Cluster)
	mockK8Cluster.CreateK8ObjectFunc = func(ctx context.Context, object sdk.Object) error {
		return errors.New("create error")
	}
	err := testController.CreateIfNotExist(context.Background(), &app)
	assert.EqualError(t, err, "create error")
}

func TestJobManagerCreateAlreadyExists(t *testing.T) {
	testController := getJMControllerForTest()
	app := getFlinkTestApp()
	mockK8Cluster := testController.k8Cluster.(*k8mock.K8Cluster)
	ctr := 0
	mockK8Cluster.CreateK8ObjectFunc = func(ctx context.Context, object sdk.Object) error {
		ctr++
		return k8sErrors.NewAlreadyExists(schema.GroupResource{}, "")
	}
	err := testController.CreateIfNotExist(context.Background(), &app)
	assert.Equal(t, ctr, 3)
	assert.Nil(t, err)
}
