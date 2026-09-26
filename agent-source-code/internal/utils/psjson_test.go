package utils

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type psJSONTestItem struct {
	Name string `json:"Name"`
}

func TestUnmarshalPSJSONArray_Array(t *testing.T) {
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte(`[{"Name":"a"},{"Name":"b"}]`), &got)
	require.NoError(t, err)
	assert.Equal(t, []psJSONTestItem{{Name: "a"}, {Name: "b"}}, got)
}

func TestUnmarshalPSJSONArray_SingleObject(t *testing.T) {
	// PowerShell's ConvertTo-Json emits a bare object (not a one-element
	// array) when the source collection has exactly one element.
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte(`{"Name":"solo"}`), &got)
	require.NoError(t, err)
	assert.Equal(t, []psJSONTestItem{{Name: "solo"}}, got)
}

func TestUnmarshalPSJSONArray_Empty(t *testing.T) {
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte(``), &got)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUnmarshalPSJSONArray_WhitespaceOnly(t *testing.T) {
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte("   \n  "), &got)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUnmarshalPSJSONArray_Null(t *testing.T) {
	// PowerShell emits the literal "null" for an empty collection.
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte(`null`), &got)
	require.NoError(t, err)
	assert.Nil(t, got)
}

func TestUnmarshalPSJSONArray_EmptyArray(t *testing.T) {
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte(`[]`), &got)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestUnmarshalPSJSONArray_Garbage(t *testing.T) {
	var got []psJSONTestItem
	err := UnmarshalPSJSONArray([]byte(`not json`), &got)
	assert.Error(t, err)
}
