package templates

import (
	"context"
	"os"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

const testNs = "testns"

var (
	k8sConfig *rest.Config
	k8sClient kubernetes.Interface
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestMain(m *testing.M) {
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	defer tearDown()

	setUp()

	return m.Run()
}

func setUp() {
	testEnv = &envtest.Environment{}

	var err error
	k8sConfig, err = testEnv.Start()

	if err != nil {
		panic(err.Error())
	}

	k8sClient, err = kubernetes.NewForConfig(k8sConfig)
	if err != nil {
		panic(err.Error())
	}

	ctx, cancel = context.WithCancel(context.TODO())

	// SetUp test resources

	// test namespace
	ns := corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNs,
		},
	}

	_, err = k8sClient.CoreV1().Namespaces().Create(ctx, &ns, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	// sample secret
	secret := corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Secret",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testsecret",
		},
		StringData: map[string]string{
			"secretkey1": "secretkey1Val",
			"secretkey2": "secretkey2Val",
		},
		Type: "Opaque",
	}

	_, err = k8sClient.CoreV1().Secrets(testNs).Create(ctx, &secret, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}

	// sample configmap
	configmap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "testconfigmap",
		},
		Data: map[string]string{
			"cmkey1":         "cmkey1Val",
			"cmkey2":         "cmkey2Val",
			"ingressSources": "[10.10.10.10, 1.1.1.1]",
		},
	}

	_, err = k8sClient.CoreV1().ConfigMaps(testNs).Create(ctx, &configmap, metav1.CreateOptions{})
	if err != nil {
		panic(err.Error())
	}
}

func tearDown() {
	cancel()

	err := testEnv.Stop()
	if err != nil {
		panic(err.Error())
	}
}
