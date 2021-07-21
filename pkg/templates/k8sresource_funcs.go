// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"context"
	base64 "encoding/base64"
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// retrieves the value of the key in the given Secret, namespace.
func fromSecret(namespace string, secretname string, key string) (string, error) {
	glog.V(glogDefLvl).Infof("fromSecret for namespace: %v, secretname: %v, key:%v", namespace, secretname, key)

	secretsClient := (*kubeClient).CoreV1().Secrets(namespace)
	secret, getErr := secretsClient.Get(context.TODO(), secretname, metav1.GetOptions{})

	if getErr != nil {
		glog.Errorf("Error Getting secret:  %v", getErr)
		err := fmt.Errorf("failed to get the secret %s from %s: %w", secretname, namespace, getErr)

		return "", err
	}
	glog.V(glogDefLvl).Infof("Secret is %v", secret)

	keyVal := secret.Data[key]
	glog.V(glogDefLvl).Infof("Secret Key:%v, Value: %v", key, keyVal)

	// when using corev1 secret api, the data is returned decoded ,
	// re-encododing to be able to use it in the referencing secret
	sEnc := base64.StdEncoding.EncodeToString(keyVal)
	glog.V(glogDefLvl).Infof("encoded secret Key:%v, Value: %v", key, sEnc)

	return sEnc, nil
}

// retrieves value for the key in the given Configmap, namespace.
func fromConfigMap(namespace string, cmapname string, key string) (string, error) {
	glog.V(glogDefLvl).Infof("fromConfigMap for namespace: %v, configmap name: %v, key:%v", namespace, cmapname, key)

	configmapsClient := (*kubeClient).CoreV1().ConfigMaps(namespace)
	configmap, getErr := configmapsClient.Get(context.TODO(), cmapname, metav1.GetOptions{})

	if getErr != nil {
		glog.Errorf("Error getting configmap:  %v", getErr)
		err := fmt.Errorf("failed getting the ConfigMap %s from %s: %w", cmapname, namespace, getErr)

		return "", err
	}
	glog.V(glogDefLvl).Infof("Configmap is %v", configmap)

	keyVal := configmap.Data[key]
	glog.V(glogDefLvl).Infof("Configmap Key:%v, Value: %v", key, keyVal)

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
