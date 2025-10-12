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

package database

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/pashagolub/pgxmock/v4"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestInitSchema(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS configmaps`).
		WillReturnResult(pgxmock.NewResult("CREATE TABLE", 0))

	err = client.InitSchema(context.Background())
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestInitSchema_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectExec(`CREATE TABLE IF NOT EXISTS configmaps`).
		WillReturnError(assert.AnError)

	err = client.InitSchema(context.Background())
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to initialize schema")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestSaveConfigMap_Insert(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
			Annotations: map[string]string{
				"description": "test configmap",
			},
		},
		Data: map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
	}

	mock.ExpectExec(`INSERT INTO configmaps`).
		WithArgs(
			"test-configmap",
			"default",
			map[string]string{"key1": "value1", "key2": "value2"},
			map[string]string{"app": "test"},
			map[string]string{"description": "test configmap"},
			"test-mirror",
			"default",
		).
		WillReturnResult(pgxmock.NewResult("INSERT", 1))

	err = client.SaveConfigMap(context.Background(), configMap, "test-mirror", "default")
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestSaveConfigMap_Update(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
			Labels: map[string]string{
				"app": "test",
			},
		},
		Data: map[string]string{
			"key1": "updated-value",
		},
	}

	mock.ExpectExec(`INSERT INTO configmaps`).
		WithArgs(
			"test-configmap",
			"default",
			map[string]string{"key1": "updated-value"},
			map[string]string{"app": "test"},
			pgxmock.AnyArg(),
			"test-mirror",
			"default",
		).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	err = client.SaveConfigMap(context.Background(), configMap, "test-mirror", "default")
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestSaveConfigMap_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: "default",
		},
		Data: map[string]string{
			"key": "value",
		},
	}

	mock.ExpectExec(`INSERT INTO configmaps`).
		WithArgs(
			"test-configmap",
			"default",
			map[string]string{"key": "value"},
			pgxmock.AnyArg(),
			pgxmock.AnyArg(),
			"test-mirror",
			"default",
		).
		WillReturnError(assert.AnError)

	err = client.SaveConfigMap(context.Background(), configMap, "test-mirror", "default")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to save ConfigMap")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestDeleteConfigMap_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectExec(`DELETE FROM configmaps`).
		WithArgs("test-configmap", "default", "test-mirror", "default").
		WillReturnResult(pgxmock.NewResult("DELETE", 1))

	err = client.DeleteConfigMap(context.Background(), "test-configmap", "default", "test-mirror", "default")
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestDeleteConfigMap_NotFound(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectExec(`DELETE FROM configmaps`).
		WithArgs("nonexistent", "default", "test-mirror", "default").
		WillReturnResult(pgxmock.NewResult("DELETE", 0))

	err = client.DeleteConfigMap(context.Background(), "nonexistent", "default", "test-mirror", "default")
	assert.Error(t, err)
	assert.Equal(t, pgx.ErrNoRows, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestDeleteConfigMap_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectExec(`DELETE FROM configmaps`).
		WithArgs("test-configmap", "default", "test-mirror", "default").
		WillReturnError(assert.AnError)

	err = client.DeleteConfigMap(context.Background(), "test-configmap", "default", "test-mirror", "default")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to delete ConfigMap")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestGetConfigMaps_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	rows := pgxmock.NewRows([]string{
		"name", "namespace", "data", "labels", "annotations",
		"configmirror_name", "configmirror_namespace",
	}).
		AddRow(
			"test-cm-1", "default",
			map[string]string{"key1": "value1"},
			map[string]string{"app": "test"},
			map[string]string{"description": "test"},
			"test-mirror", "default",
		).
		AddRow(
			"test-cm-2", "default",
			map[string]string{"key2": "value2"},
			map[string]string{"app": "test"},
			map[string]string{},
			"test-mirror", "default",
		)

	mock.ExpectQuery(`SELECT name, namespace, data, labels, annotations`).
		WithArgs("test-mirror", "default").
		WillReturnRows(rows)

	records, err := client.GetConfigMaps(context.Background(), "test-mirror", "default")
	assert.NoError(t, err)
	assert.Len(t, records, 2)

	assert.Equal(t, "test-cm-1", records[0].Name)
	assert.Equal(t, "default", records[0].Namespace)
	assert.Equal(t, map[string]string{"key1": "value1"}, records[0].Data)
	assert.Equal(t, map[string]string{"app": "test"}, records[0].Labels)

	assert.Equal(t, "test-cm-2", records[1].Name)
	assert.Equal(t, map[string]string{"key2": "value2"}, records[1].Data)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestGetConfigMaps_Empty(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	rows := pgxmock.NewRows([]string{
		"name", "namespace", "data", "labels", "annotations",
		"configmirror_name", "configmirror_namespace",
	})

	mock.ExpectQuery(`SELECT name, namespace, data, labels, annotations`).
		WithArgs("test-mirror", "default").
		WillReturnRows(rows)

	records, err := client.GetConfigMaps(context.Background(), "test-mirror", "default")
	assert.NoError(t, err)
	assert.Len(t, records, 0)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestGetConfigMaps_QueryError(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectQuery(`SELECT name, namespace, data, labels, annotations`).
		WithArgs("test-mirror", "default").
		WillReturnError(assert.AnError)

	records, err := client.GetConfigMaps(context.Background(), "test-mirror", "default")
	assert.Error(t, err)
	assert.Nil(t, records)
	assert.Contains(t, err.Error(), "failed to query ConfigMaps")

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestPing_Success(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectPing()

	err = client.Ping(context.Background())
	assert.NoError(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestPing_Error(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)
	defer mock.Close()

	client := &Client{pool: mock}

	mock.ExpectPing().WillReturnError(assert.AnError)

	err = client.Ping(context.Background())
	assert.Error(t, err)

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestClose(t *testing.T) {
	mock, err := pgxmock.NewPool()
	assert.NoError(t, err)

	client := &Client{pool: mock}

	mock.ExpectClose()

	client.Close()

	err = mock.ExpectationsWereMet()
	assert.NoError(t, err)
}

func TestClose_NilPool(t *testing.T) {
	client := &Client{pool: nil}

	// Should not panic with nil pool
	assert.NotPanics(t, func() {
		client.Close()
	})
}
