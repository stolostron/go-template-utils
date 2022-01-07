// Copyright (c) 2020 Red Hat, Inc.
// Copyright Contributors to the Open Cluster Management project

package templates

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"fmt"

	"github.com/golang/glog"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// retrieves the value of the key in the given Secret, namespace.
func (t *TemplateResolver) fromSecret(namespace string, secretname string, key string) (string, error) {
	glog.V(glogDefLvl).Infof("fromSecret for namespace: %v, secretname: %v, key:%v", namespace, secretname, key)

	ns, err := t.getNamespace("fromSecret", namespace)
	if err != nil {
		return "", err
	}

	secretsClient := (*t.kubeClient).CoreV1().Secrets(ns)
	secret, getErr := secretsClient.Get(context.TODO(), secretname, metav1.GetOptions{})

	if getErr != nil {
		glog.Errorf("Error Getting secret:  %v", getErr)
		err := fmt.Errorf("failed to get the secret %s from %s: %w", secretname, ns, getErr)

		return "", err
	}

	keyVal := secret.Data[key]

	// when using corev1 secret api, the data is returned decoded ,
	// re-encododing to be able to use it in the referencing secret
	sEnc := base64.StdEncoding.EncodeToString(keyVal)

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
	glog.V(glogDefLvl).Infof("fromConfigMap for namespace: %v, configmap name: %v, key:%v", namespace, cmapname, key)

	ns, err := t.getNamespace("fromConfigMap", namespace)
	if err != nil {
		return "", err
	}

	configmapsClient := (*t.kubeClient).CoreV1().ConfigMaps(ns)
	configmap, getErr := configmapsClient.Get(context.TODO(), cmapname, metav1.GetOptions{})

	if getErr != nil {
		glog.Errorf("Error getting configmap:  %v", getErr)
		err := fmt.Errorf("failed getting the ConfigMap %s from %s: %w", cmapname, ns, getErr)

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

// protect encrypts the input value using AES-CBC. If a salt is set on t.config.Salt, it will prefix the plaintext
// value before it is encrypted. The returned value is in the format of `$ocm_encrypted:<base64 of encrypted string>`.
// An error is returned if the AES key is invalid.
func (t *TemplateResolver) protect(value string) (string, error) {
	if value == "" {
		return value, nil
	}

	block, err := aes.NewCipher(t.config.AESKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAESKey, err)
	}

	// This is already validated in the NewResolver method, but is checked again in case that method was bypassed
	// to avoid a panic.
	if len(t.config.InitializationVector) != IVSize {
		return "", ErrInvalidIV
	}

	blockSize := block.BlockSize()
	blockMode := cipher.NewCBCEncrypter(block, t.config.InitializationVector)

	valueBytes := []byte(value)
	valueBytes = pkcs7Pad(valueBytes, blockSize)

	encryptedValue := make([]byte, len(valueBytes))
	blockMode.CryptBlocks(encryptedValue, valueBytes)

	return protectedPrefix + base64.StdEncoding.EncodeToString(encryptedValue), nil
}

// decrypt will decrypt a string that was encrypted using the protect method. An error is returned if the base64 or
// the AES key is invalid.
func (t *TemplateResolver) decrypt(value string) (string, error) {
	decodedValue, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return "", fmt.Errorf("%s: %w: %v", value, ErrInvalidB64OfEncrypted, err)
	}

	block, err := aes.NewCipher(t.config.AESKey)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidAESKey, err)
	}

	// This is already validated in the NewResolver method, but is checked again in case that method was bypassed
	// to avoid a panic.
	if len(t.config.InitializationVector) != IVSize {
		return "", ErrInvalidIV
	}

	blockMode := cipher.NewCBCDecrypter(block, t.config.InitializationVector)

	decryptedValue := make([]byte, len(decodedValue))
	blockMode.CryptBlocks(decryptedValue, decodedValue)

	decryptedValue, err = pkcs7Unpad(decryptedValue)
	if err != nil {
		return "", err
	}

	return string(decryptedValue), nil
}

// pkcs7Pad right-pads the given value to match the input block size for AES encryption. The padding
// ranges from 1 byte to the number of bytes equal to the block size.
// Inspired from https://gist.github.com/huyinghuan/7bf174017bf54efb91ece04a48589b22.
func pkcs7Pad(value []byte, blockSize int) []byte {
	// Determine the amount of padding that is required in order to make the plaintext value
	// divisible by the block size. If it is already divisible by the block size, then the padding
	// amount will be a whole block. This is to ensure there is always padding.
	paddingAmount := blockSize - (len(value) % blockSize)
	// Create a new byte slice that can contain the plaintext value and the padding.
	paddedValue := make([]byte, len(value)+paddingAmount)
	// Copy the original value into the new byte slice.
	copy(paddedValue, value)
	// Add the padding to the new byte slice. Each padding byte is the byte representation of the
	// padding amount. This ensures that the last byte of the padded plaintext value refers to the
	// amount of padding to remove when unpadded.
	copy(paddedValue[len(value):], bytes.Repeat([]byte{byte(paddingAmount)}, paddingAmount))

	return paddedValue
}

// pkcs7Unpad unpads data from the given padded value. The last byte must be the number of bytes of padding to remove.
// The ErrInvalidPKCS7Padding error is returned if the value does not have valid padding. This could happen if the user
// did not use the "protect" method to encrypt the data and provided an invalid value.
// Inspired from https://gist.github.com/huyinghuan/7bf174017bf54efb91ece04a48589b22.
func pkcs7Unpad(paddedValue []byte) ([]byte, error) {
	// Determine the amount of padding bytes to remove by checking the value of the last byte.
	lastByte := paddedValue[len(paddedValue)-1]
	numPaddingBytes := int(lastByte)

	// Verify that the last byte is a valid padding length.
	if numPaddingBytes == 0 || numPaddingBytes > len(paddedValue) {
		return nil, fmt.Errorf("%w: the padding length is invalid", ErrInvalidPKCS7Padding)
	}

	// Verify that the declared padding is valid by checking that the padding is all the same byte.
	// i > 1 is the conditional to avoid checking that the last byte is equal to itself.
	for i := numPaddingBytes; i > 1; i-- {
		if paddedValue[len(paddedValue)-i] != lastByte {
			return nil, fmt.Errorf("%w: not all the padding bytes match", ErrInvalidPKCS7Padding)
		}
	}

	// Remove the padding from the byte slice.
	return paddedValue[:len(paddedValue)-numPaddingBytes], nil
}
