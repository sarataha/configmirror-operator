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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mirrorv1alpha1 "github.com/sarataha/configmirror-operator/api/v1alpha1"
)

var _ = Describe("ConfigMirror Controller Integration Tests", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a ConfigMirror resource", func() {
		var (
			ctx              context.Context
			configMirrorName string
			sourceNamespace  string
			targetNamespace1 string
			targetNamespace2 string
			reconciler       *ConfigMirrorReconciler
		)

		BeforeEach(func() {
			ctx = context.Background()
			configMirrorName = "test-configmirror-" + randString(5)
			sourceNamespace = "default"
			targetNamespace1 = "test-target-1-" + randString(5)
			targetNamespace2 = "test-target-2-" + randString(5)

			By("Creating target namespaces")
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: targetNamespace1},
			})).To(Succeed())
			Expect(k8sClient.Create(ctx, &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: targetNamespace2},
			})).To(Succeed())

			reconciler = &ConfigMirrorReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				DBClient: nil,
			}
		})

		AfterEach(func() {
			By("Cleaning up ConfigMirror")
			configMirror := &mirrorv1alpha1.ConfigMirror{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: configMirrorName, Namespace: sourceNamespace}, configMirror)
			if err == nil {
				Expect(k8sClient.Delete(ctx, configMirror)).To(Succeed())
			}

			By("Cleaning up namespaces")
			ns1 := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: targetNamespace1}, ns1); err == nil {
				Expect(k8sClient.Delete(ctx, ns1)).To(Succeed())
			}
			ns2 := &corev1.Namespace{}
			if err := k8sClient.Get(ctx, types.NamespacedName{Name: targetNamespace2}, ns2); err == nil {
				Expect(k8sClient.Delete(ctx, ns2)).To(Succeed())
			}
		})

		It("should add finalizer when ConfigMirror is created", func() {
			By("Creating ConfigMirror")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TargetNamespaces: []string{targetNamespace1},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Reconciling the resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking finalizer was added")
			Eventually(func() []string {
				updated := &mirrorv1alpha1.ConfigMirror{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				}, updated)
				if err != nil {
					return nil
				}
				return updated.Finalizers
			}, timeout, interval).Should(ContainElement("mirror.pawapay.io/finalizer"))
		})

		It("should replicate ConfigMaps matching label selector to target namespaces", func() {
			By("Creating source ConfigMap with matching labels")
			sourceConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-cm-" + randString(5),
					Namespace: sourceNamespace,
					Labels: map[string]string{
						"app": "test",
						"env": "dev",
					},
				},
				Data: map[string]string{
					"config.yaml": "test: value",
					"app.conf":    "setting=1",
				},
			}
			Expect(k8sClient.Create(ctx, sourceConfigMap)).To(Succeed())

			By("Creating ConfigMirror")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TargetNamespaces: []string{targetNamespace1, targetNamespace2},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Reconciling")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying ConfigMap replicated to target namespace 1")
			Eventually(func() error {
				replica := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      sourceConfigMap.Name,
					Namespace: targetNamespace1,
				}, replica)
			}, timeout, interval).Should(Succeed())

			By("Verifying ConfigMap replicated to target namespace 2")
			Eventually(func() error {
				replica := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      sourceConfigMap.Name,
					Namespace: targetNamespace2,
				}, replica)
			}, timeout, interval).Should(Succeed())

			By("Verifying replica data matches source")
			replica := &corev1.ConfigMap{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: targetNamespace1,
			}, replica)).To(Succeed())
			Expect(replica.Data).To(Equal(sourceConfigMap.Data))
			Expect(replica.Labels).To(HaveKeyWithValue("mirror.pawapay.io/source-namespace", sourceNamespace))
			Expect(replica.Labels).To(HaveKeyWithValue("mirror.pawapay.io/source-name", sourceConfigMap.Name))
		})

		It("should not replicate ConfigMaps with non-matching labels", func() {
			By("Creating source ConfigMap with non-matching labels")
			nonMatchingCM := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "non-matching-cm-" + randString(5),
					Namespace: sourceNamespace,
					Labels: map[string]string{
						"app": "other",
					},
				},
				Data: map[string]string{
					"data": "value",
				},
			}
			Expect(k8sClient.Create(ctx, nonMatchingCM)).To(Succeed())

			By("Creating ConfigMirror with specific selector")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TargetNamespaces: []string{targetNamespace1},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Reconciling")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying non-matching ConfigMap was NOT replicated")
			Consistently(func() bool {
				replica := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      nonMatchingCM.Name,
					Namespace: targetNamespace1,
				}, replica)
				return errors.IsNotFound(err)
			}, time.Second*2, interval).Should(BeTrue())
		})

		It("should update replicated ConfigMaps when source changes", func() {
			By("Creating source ConfigMap")
			sourceConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "update-test-cm-" + randString(5),
					Namespace: sourceNamespace,
					Labels: map[string]string{
						"app": "test",
					},
				},
				Data: map[string]string{
					"key": "original-value",
				},
			}
			Expect(k8sClient.Create(ctx, sourceConfigMap)).To(Succeed())

			By("Creating ConfigMirror")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TargetNamespaces: []string{targetNamespace1},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for initial replication")
			Eventually(func() error {
				replica := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      sourceConfigMap.Name,
					Namespace: targetNamespace1,
				}, replica)
			}, timeout, interval).Should(Succeed())

			By("Updating source ConfigMap data")
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      sourceConfigMap.Name,
				Namespace: sourceNamespace,
			}, sourceConfigMap)).To(Succeed())
			sourceConfigMap.Data["key"] = "updated-value"
			sourceConfigMap.Data["new-key"] = "new-value"
			Expect(k8sClient.Update(ctx, sourceConfigMap)).To(Succeed())

			By("Reconciling after update")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying replica was updated")
			Eventually(func() map[string]string {
				replica := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      sourceConfigMap.Name,
					Namespace: targetNamespace1,
				}, replica)
				if err != nil {
					return nil
				}
				return replica.Data
			}, timeout, interval).Should(Equal(map[string]string{
				"key":     "updated-value",
				"new-key": "new-value",
			}))
		})

		It("should clean up replicated ConfigMaps when source is deleted", func() {
			By("Creating source ConfigMap")
			sourceConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "delete-test-cm-" + randString(5),
					Namespace: sourceNamespace,
					Labels: map[string]string{
						"app": "test",
					},
				},
				Data: map[string]string{
					"data": "value",
				},
			}
			Expect(k8sClient.Create(ctx, sourceConfigMap)).To(Succeed())

			By("Creating ConfigMirror")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TargetNamespaces: []string{targetNamespace1},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Initial reconciliation")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Waiting for replication")
			Eventually(func() error {
				replica := &corev1.ConfigMap{}
				return k8sClient.Get(ctx, types.NamespacedName{
					Name:      sourceConfigMap.Name,
					Namespace: targetNamespace1,
				}, replica)
			}, timeout, interval).Should(Succeed())

			By("Deleting source ConfigMap")
			Expect(k8sClient.Delete(ctx, sourceConfigMap)).To(Succeed())

			By("Reconciling after deletion")
			_, err = reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Verifying replica was deleted (orphan cleanup)")
			Eventually(func() bool {
				replica := &corev1.ConfigMap{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      sourceConfigMap.Name,
					Namespace: targetNamespace1,
				}, replica)
				return errors.IsNotFound(err)
			}, timeout, interval).Should(BeTrue())
		})

		It("should update status with replicated ConfigMaps", func() {
			By("Creating source ConfigMap")
			sourceConfigMap := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "status-test-cm-" + randString(5),
					Namespace: sourceNamespace,
					Labels: map[string]string{
						"app": "test",
					},
				},
				Data: map[string]string{
					"data": "value",
				},
			}
			Expect(k8sClient.Create(ctx, sourceConfigMap)).To(Succeed())

			By("Creating ConfigMirror")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "test"},
					},
					TargetNamespaces: []string{targetNamespace1},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Reconciling")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())

			By("Checking status was updated")
			Eventually(func() []mirrorv1alpha1.ReplicatedConfigMap {
				updated := &mirrorv1alpha1.ConfigMirror{}
				err := k8sClient.Get(ctx, types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				}, updated)
				if err != nil {
					return nil
				}
				return updated.Status.ReplicatedConfigMaps
			}, timeout, interval).Should(HaveLen(1))

			By("Verifying status contains ConfigMap details")
			updated := &mirrorv1alpha1.ConfigMirror{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name:      configMirrorName,
				Namespace: sourceNamespace,
			}, updated)).To(Succeed())
			Expect(updated.Status.ReplicatedConfigMaps[0].Name).To(Equal(sourceConfigMap.Name))
			Expect(updated.Status.ReplicatedConfigMaps[0].SourceNamespace).To(Equal(sourceNamespace))
			Expect(updated.Status.ReplicatedConfigMaps[0].TargetNamespaces).To(ConsistOf(targetNamespace1))
		})

		It("should handle invalid label selector gracefully", func() {
			By("Creating ConfigMirror with invalid selector")
			configMirror := &mirrorv1alpha1.ConfigMirror{
				ObjectMeta: metav1.ObjectMeta{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
				Spec: mirrorv1alpha1.ConfigMirrorSpec{
					Selector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "invalid",
								Operator: "InvalidOperator",
							},
						},
					},
					TargetNamespaces: []string{targetNamespace1},
				},
			}
			Expect(k8sClient.Create(ctx, configMirror)).To(Succeed())

			By("Reconciling")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      configMirrorName,
					Namespace: sourceNamespace,
				},
			})
			Expect(err).To(HaveOccurred())
		})

		It("should return nil when ConfigMirror resource is not found", func() {
			By("Reconciling non-existent resource")
			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "non-existent",
					Namespace: sourceNamespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})

func randString(n int) string {
	const letters = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = letters[time.Now().UnixNano()%int64(len(letters))]
		time.Sleep(time.Nanosecond)
	}
	return string(b)
}
