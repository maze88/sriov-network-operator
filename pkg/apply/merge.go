// Copyright 2025 sriov-network-device-plugin authors
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
//
// SPDX-License-Identifier: Apache-2.0

package apply

import (
	"fmt"

	"github.com/pkg/errors"

	uns "k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	constants "github.com/k8snetworkplumbingwg/sriov-network-operator/pkg/consts"
)

// MergeMetadataForUpdate merges the read-only fields of metadata.
// This is to be able to do a a meaningful comparison in apply,
// since objects created on runtime do not have these fields populated.
func MergeMetadataForUpdate(current, updated *uns.Unstructured) error {
	mergeAnnotations(current, updated)
	mergeLabels(current, updated)
	updated.SetResourceVersion(current.GetResourceVersion())

	return nil
}

// MergeObjectForUpdate prepares a "desired" object to be updated.
// Some objects, such as Deployments and Services require
// some semantic-aware updates
func MergeObjectForUpdate(current, updated *uns.Unstructured) error {
	if err := MergeDeploymentForUpdate(current, updated); err != nil {
		return err
	}

	if err := MergeServiceForUpdate(current, updated); err != nil {
		return err
	}

	if err := MergeServiceAccountForUpdate(current, updated); err != nil {
		return err
	}

	if err := MergeWebhookForUpdate(current, updated); err != nil {
		return err
	}

	// For all object types, merge metadata.
	// Run this last, in case any of the more specific merge logic has
	// changed "updated"
	MergeMetadataForUpdate(current, updated)

	return nil
}

const (
	deploymentRevisionAnnotation = "deployment.kubernetes.io/revision"
)

// MergeDeploymentForUpdate updates Deployment objects.
// We merge annotations, keeping ours except the Deployment Revision annotation.
func MergeDeploymentForUpdate(current, updated *uns.Unstructured) error {
	gvk := updated.GroupVersionKind()
	if gvk.Group == "apps" && gvk.Kind == "Deployment" {
		// Copy over the revision annotation from current up to updated
		// otherwise, updated would win, and this annotation is "special" and
		// needs to be preserved
		curAnnotations := current.GetAnnotations()
		updatedAnnotations := updated.GetAnnotations()
		if updatedAnnotations == nil {
			updatedAnnotations = map[string]string{}
		}

		anno, ok := curAnnotations[deploymentRevisionAnnotation]
		if ok {
			updatedAnnotations[deploymentRevisionAnnotation] = anno
		}

		updated.SetAnnotations(updatedAnnotations)
	}

	return nil
}

// MergeServiceForUpdate ensures the clusterip is never written to
func MergeServiceForUpdate(current, updated *uns.Unstructured) error {
	gvk := updated.GroupVersionKind()
	if gvk.Group == "" && gvk.Kind == "Service" {
		clusterIP, found, err := uns.NestedString(current.Object, "spec", "clusterIP")
		if err != nil {
			return err
		}

		if found {
			return uns.SetNestedField(updated.Object, clusterIP, "spec", "clusterIP")
		}
	}

	return nil
}

// MergeServiceAccountForUpdate copies secrets from current to updated.
// This is intended to preserve the auto-generated token.
// Right now, we just copy current to updated and don't support supplying
// any secrets ourselves.
func MergeServiceAccountForUpdate(current, updated *uns.Unstructured) error {
	gvk := updated.GroupVersionKind()
	if gvk.Group == "" && gvk.Kind == constants.ServiceAccount {
		curSecrets, ok, err := uns.NestedSlice(current.Object, "secrets")
		if err != nil {
			return err
		}

		if ok {
			uns.SetNestedField(updated.Object, curSecrets, "secrets")
		}

		curImagePullSecrets, ok, err := uns.NestedSlice(current.Object, "imagePullSecrets")
		if err != nil {
			return err
		}
		if ok {
			uns.SetNestedField(updated.Object, curImagePullSecrets, "imagePullSecrets")
		}
	}
	return nil
}

// MergeWebhookForUpdate ensures the Webhook.ClientConfig.CABundle is never removed from a webhook
func MergeWebhookForUpdate(current, updated *uns.Unstructured) error {
	gvk := updated.GroupVersionKind()
	if gvk.Group != "admissionregistration.k8s.io" {
		return nil
	}

	if gvk.Kind != "ValidatingWebhookConfiguration" && gvk.Kind != "MutatingWebhookConfiguration" {
		return nil
	}

	updatedWebhooks, ok, err := uns.NestedSlice(updated.Object, "webhooks")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	currentWebhooks, ok, err := uns.NestedSlice(current.Object, "webhooks")
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	for _, updatedWebhook := range updatedWebhooks {
		updateWebhookMap := updatedWebhook.(map[string]interface{})
		caBundle, ok, err := uns.NestedString(updateWebhookMap, "clientConfig", "caBundle")
		if err != nil {
			return nil
		}

		// if the updated object already contains a CABundle, leave it as is. If it's nil, update its value with the current one
		if ok && caBundle != "" {
			continue
		}

		webhookName, ok, err := uns.NestedString(updateWebhookMap, "name")
		if err != nil {
			return err
		}

		if !ok {
			return fmt.Errorf("webhook name not found in %v", updateWebhookMap)
		}

		currentWebhook := findByName(currentWebhooks, webhookName)
		if currentWebhook == nil {
			// Webhook not yet present in the cluster
			continue
		}

		currentCABundle, ok, err := uns.NestedString(*currentWebhook, "clientConfig", "caBundle")
		if err != nil {
			return err
		}

		if !ok {
			// Cluster webook does not have a CABundle
			continue
		}

		uns.SetNestedField(updateWebhookMap, currentCABundle, "clientConfig", "caBundle")
	}

	err = uns.SetNestedSlice(updated.Object, updatedWebhooks, "webhooks")
	if err != nil {
		return err
	}

	return nil
}

func findByName(objList []interface{}, name string) *map[string]interface{} {
	for _, obj := range objList {
		objMap := obj.(map[string]interface{})
		if objMap["name"] == name {
			return &objMap
		}
	}
	return nil
}

// mergeAnnotations copies over any annotations from current to updated,
// with updated winning if there's a conflict
func mergeAnnotations(current, updated *uns.Unstructured) {
	updatedAnnotations := updated.GetAnnotations()
	curAnnotations := current.GetAnnotations()

	if curAnnotations == nil {
		curAnnotations = map[string]string{}
	}

	for k, v := range updatedAnnotations {
		curAnnotations[k] = v
	}
	if len(curAnnotations) > 0 {
		updated.SetAnnotations(curAnnotations)
	}
}

// mergeLabels copies over any labels from current to updated,
// with updated winning if there's a conflict
func mergeLabels(current, updated *uns.Unstructured) {
	updatedLabels := updated.GetLabels()
	curLabels := current.GetLabels()

	if curLabels == nil {
		curLabels = map[string]string{}
	}

	for k, v := range updatedLabels {
		curLabels[k] = v
	}
	if len(curLabels) > 0 {
		updated.SetLabels(curLabels)
	}
}

// IsObjectSupported rejects objects with configurations we don't support.
// This catches ServiceAccounts with secrets, which is valid but we don't
// support reconciling them.
func IsObjectSupported(obj *uns.Unstructured) error {
	gvk := obj.GroupVersionKind()

	// We cannot create ServiceAccounts with secrets because there's currently
	// no need and the merging logic is complex.
	// If you need this, please file an issue.
	if gvk.Group == "" && gvk.Kind == "ServiceAccount" {
		secrets, ok, err := uns.NestedSlice(obj.Object, "secrets")
		if err != nil {
			return err
		}

		if ok && len(secrets) > 0 {
			return errors.Errorf("cannot create ServiceAccount with secrets")
		}
	}

	return nil
}
