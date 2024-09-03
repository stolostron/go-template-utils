package main

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

var (
	testEnv            *envtest.Environment
	ctx                context.Context
	cancel             context.CancelFunc
	errKubectl         = errors.New("kubectl exited with error")
	kubeconfigPath     string
	savedKubeconfigEnv string
)

//go:embed testdata/*
var testfiles embed.FS

func TestMain(m *testing.M) {
	os.Exit(testMain(m))
}

func testMain(m *testing.M) int {
	defer tearDown()

	setUp()

	return m.Run()
}

func setUp() {
	savedKubeconfigEnv = os.Getenv("KUBECONFIG")

	ctx, cancel = context.WithCancel(context.TODO())

	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{"../../testdata/crds.yaml"},
	}

	k8sConfig, err := testEnv.Start()
	if err != nil {
		panic(err.Error())
	}

	kubeconfigFile, err := os.CreateTemp("", "test-*-kubeconfig")
	if err != nil {
		panic(err.Error())
	}

	if err = writeKubeconfig(kubeconfigFile, k8sConfig); err != nil {
		panic(err.Error())
	}

	kubeconfigPath = kubeconfigFile.Name()
	os.Setenv("KUBECONFIG", kubeconfigPath)

	entries, err := testfiles.ReadDir("testdata/setup")
	if err != nil {
		panic(err.Error())
	}

	//nolint: forbidigo
	for _, e := range entries {
		p := filepath.Join("testdata", "setup", e.Name())
		fmt.Println("Applying setup file:", p)

		out, err := kubectl("apply", "-f", p)
		if err != nil {
			panic(err)
		}

		fmt.Print(out)
	}
}

func writeKubeconfig(f *os.File, restConfig *rest.Config) error {
	identifier := "template-resolver-envtest"

	kubeconfig := api.NewConfig()

	cluster := api.NewCluster()
	cluster.Server = restConfig.Host
	cluster.CertificateAuthorityData = restConfig.CAData
	kubeconfig.Clusters[identifier] = cluster

	authInfo := api.NewAuthInfo()
	authInfo.ClientCertificateData = restConfig.CertData
	authInfo.ClientKeyData = restConfig.KeyData
	kubeconfig.AuthInfos[identifier] = authInfo

	apiContext := api.NewContext()
	apiContext.Cluster = identifier
	apiContext.AuthInfo = identifier
	kubeconfig.Contexts[identifier] = apiContext
	kubeconfig.CurrentContext = identifier

	configBytes, err := clientcmd.Write(*kubeconfig)
	if err != nil {
		return err
	}

	_, err = f.Write(configBytes)
	if err != nil {
		return err
	}

	return nil
}

func kubectl(args ...string) (string, error) {
	args = append([]string{"--kubeconfig=" + kubeconfigPath}, args...)

	output, err := exec.Command("kubectl", args...).Output()

	var exitError *exec.ExitError

	if errors.As(err, &exitError) {
		if exitError.Stderr == nil {
			return string(output), err
		}

		return string(output), fmt.Errorf("%w: %s", errKubectl, exitError.Stderr)
	}

	return string(output), err
}

func tearDown() {
	os.Setenv("KUBECONFIG", savedKubeconfigEnv)

	if err := os.Remove(kubeconfigPath); err != nil {
		//nolint: forbidigo
		fmt.Printf("Could not remove temporary kubeconfig at %v; error: %v\n", kubeconfigPath, err)
	}

	cancel()

	err := testEnv.Stop()
	if err != nil {
		panic(err.Error())
	}
}
