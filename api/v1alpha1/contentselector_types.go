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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// ContentSelectorSpec определяет желаемое состояние Content Selector в Nexus.
type ContentSelectorSpec struct {
	// Имя Content Selector.
	Name string `json:"name"`

	// Описание Content Selector.
	Description string `json:"description"`

	// Выражение для выбора контента.
	Expression string `json:"expression"`
}

// ContentSelectorStatus определяет текущее состояние Content Selector.
type ContentSelectorStatus struct {
	// Conditions содержит список условий, описывающих состояние ресурса.
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ContentSelector — это CRD для управления Content Selector в Nexus.
type ContentSelector struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ContentSelectorSpec   `json:"spec,omitempty"`
	Status ContentSelectorStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ContentSelectorList содержит список Content Selector.
type ContentSelectorList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ContentSelector `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ContentSelector{}, &ContentSelectorList{})
}
