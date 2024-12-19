// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/klog"
)

func (t *TemplateResolver) fromSecretHelper(
	options *ResolveOptions,
	templateResult *TemplateResult,
) func(string, string, string) (string, error) {
	return func(namespace string, name string, key string) (string, error) {
		return t.fromSecret(options, templateResult, namespace, name, key)
	}
}

// retrieves the value of the key in the given Secret, namespace.
func (t *TemplateResolver) fromSecret(
	options *ResolveOptions, templateResult *TemplateResult, namespace string, name string, key string,
) (string, error) {
	klog.V(2).Infof("fromSecret for namespace: %v, name: %v, key:%v", namespace, name, key)

	if name == "" || (options.LookupNamespace == "" && namespace == "") || key == "" {
		return "", fmt.Errorf("%w: namespace, name, and key must be specified", ErrInvalidInput)
	}

	secret, err := t.getOrList(options, templateResult, "v1", "Secret", namespace, name)
	if err != nil {
		return "", fmt.Errorf("failed to get the secret %s from %s: %w", name, namespace, err)
	}

	keyVal, _, _ := unstructured.NestedString(secret, "data", key)

	return keyVal, nil
}

func (t *TemplateResolver) fromSecretProtectedHelper(
	options *ResolveOptions,
	templateResult *TemplateResult,
) func(string, string, string) (string, error) {
	return func(namespace string, secretName string, key string) (string, error) {
		return t.fromSecretProtected(options, templateResult, namespace, secretName, key)
	}
}

// fromSecretProtected wraps fromSecret and encrypts the output value using the "protect" method.
func (t *TemplateResolver) fromSecretProtected(
	options *ResolveOptions, templateResult *TemplateResult, namespace string, secretName string, key string,
) (string, error) {
	value, err := t.fromSecret(options, templateResult, namespace, secretName, key)
	if err != nil {
		return "", err
	}

	return t.protect(options, value)
}

// copies all data in the given Secret, namespace.
func (t *TemplateResolver) copySecretDataBase(
	options *ResolveOptions, templateResult *TemplateResult, namespace string, name string,
) (map[string]interface{}, error) {
	klog.V(2).Infof("copySecretDataBase for namespace: %v, name: %v", namespace, name)

	if name == "" || (options.LookupNamespace == "" && namespace == "") {
		return nil, fmt.Errorf("%w: namespace and name must be specified", ErrInvalidInput)
	}

	secret, err := t.getOrList(options, templateResult, "v1", "Secret", namespace, name)
	if err != nil {
		return nil, fmt.Errorf("failed to get the secret %s from %s: %w", name, namespace, err)
	}

	data, _, _ := unstructured.NestedMap(secret, "data")

	return data, nil
}

func (t *TemplateResolver) copySecretDataHelper(
	options *ResolveOptions, templateResult *TemplateResult,
) func(string, string) (string, error) {
	return func(namespace string, secretname string) (string, error) {
		return t.copySecretData(options, templateResult, namespace, secretname)
	}
}

// copies all data in the given Secret, namespace.
func (t *TemplateResolver) copySecretData(
	options *ResolveOptions, templateResult *TemplateResult, namespace string, secretname string,
) (string, error) {
	klog.V(2).Infof("copySecretData for namespace: %v, secretname: %v", namespace, secretname)

	data, err := t.copySecretDataBase(options, templateResult, namespace, secretname)
	if err != nil {
		return "", err
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return string(rawData), nil
}

func (t *TemplateResolver) copySecretDataProtectedHelper(
	options *ResolveOptions, templateResult *TemplateResult,
) func(string, string) (string, error) {
	return func(namespace string, secretName string) (string, error) {
		return t.copySecretDataProtected(options, templateResult, namespace, secretName)
	}
}

// copySecretDataProtected wraps copySecretData and encrypts the output value using the "protect" method.
func (t *TemplateResolver) copySecretDataProtected(
	options *ResolveOptions, templateResult *TemplateResult, namespace string, secretName string,
) (string, error) {
	data, err := t.copySecretDataBase(options, templateResult, namespace, secretName)
	if err != nil {
		return "", err
	}

	for key, val := range data {
		data[key], err = t.protect(options, fmt.Sprint(val))
		if err != nil {
			return "", err
		}
	}

	rawData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return string(rawData), nil
}

func (t *TemplateResolver) fromConfigMapHelper(
	options *ResolveOptions,
) func(string, string, string) (string, error) {
	return func(namespace string, name string, key string) (string, error) {
		return t.fromConfigMap(options, namespace, name, key)
	}
}

// retrieves value for the key in the given Configmap, namespace.
func (t *TemplateResolver) fromConfigMap(
	options *ResolveOptions, namespace string, name string, key string,
) (string, error) {
	klog.V(2).Infof("fromConfigMap for namespace: %s, name: %s, key: %s", namespace, name, key)

	if name == "" || (options.LookupNamespace == "" && namespace == "") || key == "" {
		return "", fmt.Errorf("%w: namespace, name, and key must be specified", ErrInvalidInput)
	}

	configmap, err := t.getOrList(options, nil, "v1", "ConfigMap", namespace, name)
	if err != nil {
		err := fmt.Errorf("failed getting the ConfigMap %s from %s: %w", name, namespace, err)

		return "", err
	}

	keyVal, _, _ := unstructured.NestedString(configmap, "data", key)

	return keyVal, nil
}

func (t *TemplateResolver) copyConfigMapDataHelper(options *ResolveOptions) func(string, string) (string, error) {
	return func(namespace string, name string) (string, error) {
		return t.copyConfigMapData(options, namespace, name)
	}
}

// copies data values in the given Configmap, namespace.
func (t *TemplateResolver) copyConfigMapData(
	options *ResolveOptions, namespace string, name string,
) (string, error) {
	klog.V(2).Infof("copyConfigMapData for namespace: %s, name: %s", namespace, name)

	if name == "" || (options.LookupNamespace == "" && namespace == "") {
		return "", fmt.Errorf("%w: namespace and name must be specified", ErrInvalidInput)
	}

	configmap, err := t.getOrList(options, nil, "v1", "ConfigMap", namespace, name)
	if err != nil {
		return "", fmt.Errorf("failed getting the ConfigMap %s from %s: %w", name, namespace, err)
	}

	data, _, _ := unstructured.NestedMap(configmap, "data")

	rawData, err := json.Marshal(data)
	if err != nil {
		return "", err
	}

	return string(rawData), nil
}

// convenience functions to base64 encode string values
// for setting in value in Referencing Secret resources.
func base64encode(v string) string {
	return base64.StdEncoding.EncodeToString([]byte(v))
}

func base64decode(v string) string {
	data, err := base64.StdEncoding.DecodeString(v)
	if err != nil {
		return err.Error()
	}

	return string(data)
}
