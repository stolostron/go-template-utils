// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	"encoding/base64"
	"fmt"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
)

// retrieves the value of the key in the given Secret, namespace.
func (t *TemplateResolver) fromSecret(namespace string, secretname string, key string) (string, error) {
	klog.V(2).Infof("fromSecret for namespace: %v, secretname: %v, key:%v", namespace, secretname, key)

	ns, err := t.getNamespace("fromSecret", namespace)
	if err != nil {
		return "", err
	}

	secretsClient := (*t.kubeClient).CoreV1().Secrets(ns)
	secret, getErr := secretsClient.Get(context.TODO(), secretname, metav1.GetOptions{})

	if getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			// Add to the referenced objects if the object isn't found since the consumer may want to watch the object
			// and resolve the templates again once it is present.
			t.addToReferencedObjects("/v1", "Secret", ns, secretname)
		}

		klog.Errorf("Error Getting secret:  %v", getErr)
		err := fmt.Errorf("failed to get the secret %s from %s: %w", secretname, ns, getErr)

		return "", err
	}

	keyVal := secret.Data[key]

	// when using corev1 secret api, the data is returned decoded ,
	// re-encododing to be able to use it in the referencing secret
	sEnc := base64.StdEncoding.EncodeToString(keyVal)

	// add this Secret to list of  Objs referenced by the template
	t.addToReferencedObjects("/v1", "Secret", secret.Namespace, secret.Name)

	return sEnc, nil
}

// fromSecretProtected wraps fromSecret and encrypts the output value using the "protect" method.
func (t *TemplateResolver) fromSecretProtected(namespace string, secretName string, key string) (string, error) {
	value, err := t.fromSecret(namespace, secretName, key)
	if err != nil {
		return "", err
	}

	return t.protect(value)
}

// retrieves value for the key in the given Configmap, namespace.
func (t *TemplateResolver) fromConfigMap(namespace string, cmapname string, key string) (string, error) {
	klog.V(2).Infof("fromConfigMap for namespace: %v, configmap name: %v, key:%v", namespace, cmapname, key)

	ns, err := t.getNamespace("fromConfigMap", namespace)
	if err != nil {
		return "", err
	}

	configmapsClient := (*t.kubeClient).CoreV1().ConfigMaps(ns)
	configmap, getErr := configmapsClient.Get(context.TODO(), cmapname, metav1.GetOptions{})

	if getErr != nil {
		if k8serrors.IsNotFound(getErr) {
			// Add to the referenced objects if the object isn't found since the consumer may want to watch the object
			// and resolve the templates again once it is present.
			t.addToReferencedObjects("/v1", "ConfigMap", ns, cmapname)
		}

		klog.Errorf("Error getting configmap:  %v", getErr)
		err := fmt.Errorf("failed getting the ConfigMap %s from %s: %w", cmapname, ns, getErr)

		return "", err
	}

	klog.V(2).Infof("Configmap is %v", configmap)

	keyVal := configmap.Data[key]
	klog.V(2).Infof("Configmap Key:%v, Value: %v", key, keyVal)

	// add this Configmap to list of  Objs referenced by the template
	t.addToReferencedObjects("/v1", "ConfigMap", namespace, cmapname)

	return keyVal, nil
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
