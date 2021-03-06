package executor

import (
	"fmt"
	"io/ioutil"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/utils/pointer"

	wfv1 "github.com/argoproj/argo/pkg/apis/workflow/v1alpha1"
	"github.com/argoproj/argo/workflow/executor/mocks"
)

const (
	fakePodName     = "fake-test-pod-1234567890"
	fakeNamespace   = "default"
	fakeAnnotations = "/tmp/podannotationspath"
	fakeContainerID = "abc123"
)

func TestSaveParameters(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	mockRuntimeExecutor := mocks.ContainerRuntimeExecutor{}
	templateWithOutParam := wfv1.Template{
		Outputs: wfv1.Outputs{
			Parameters: []wfv1.Parameter{
				{
					Name: "my-out",
					ValueFrom: &wfv1.ValueFrom{
						Path: "/path",
					},
				},
			},
		},
	}
	we := WorkflowExecutor{
		PodName:            fakePodName,
		Template:           templateWithOutParam,
		ClientSet:          fakeClientset,
		Namespace:          fakeNamespace,
		PodAnnotationsPath: fakeAnnotations,
		ExecutionControl:   nil,
		RuntimeExecutor:    &mockRuntimeExecutor,
		mainContainerID:    fakeContainerID,
	}
	mockRuntimeExecutor.On("GetFileContents", fakeContainerID, "/path").Return("has a newline\n", nil)
	err := we.SaveParameters()
	assert.NoError(t, err)
	assert.Equal(t, "has a newline", *we.Template.Outputs.Parameters[0].Value)
}

// TestIsBaseImagePath tests logic of isBaseImagePath which determines if a path is coming from a
// base image layer versus a shared volumeMount.
func TestIsBaseImagePath(t *testing.T) {
	templateWithSameDir := wfv1.Template{
		Inputs: wfv1.Inputs{
			Artifacts: []wfv1.Artifact{
				{
					Name: "samedir",
					Path: "/samedir",
				},
			},
		},
		Container: &corev1.Container{},
		Outputs: wfv1.Outputs{
			Artifacts: []wfv1.Artifact{
				{
					Name: "samedir",
					Path: "/samedir",
				},
			},
		},
	}

	we := WorkflowExecutor{
		Template: templateWithSameDir,
	}
	// 1. unrelated dir/file should be captured from base image layer
	assert.True(t, we.isBaseImagePath("/foo"))

	// 2. when input and output directory is same, it should be captured from shared emptyDir
	assert.False(t, we.isBaseImagePath("/samedir"))

	// 3. when output is a sub path of input dir, it should be captured from shared emptyDir
	we.Template.Outputs.Artifacts[0].Path = "/samedir/inner"
	assert.False(t, we.isBaseImagePath("/samedir/inner"))

	// 4. when output happens to overlap with input (in name only), it should be captured from base image layer
	we.Template.Inputs.Artifacts[0].Path = "/hello.txt"
	we.Template.Outputs.Artifacts[0].Path = "/hello.txt-COINCIDENCE"
	assert.True(t, we.isBaseImagePath("/hello.txt-COINCIDENCE"))

	// 5. when output is under a user specified volumeMount, it should be captured from shared mount
	we.Template.Inputs.Artifacts = nil
	we.Template.Container.VolumeMounts = []corev1.VolumeMount{
		{
			Name:      "workdir",
			MountPath: "/user-mount",
		},
	}
	we.Template.Outputs.Artifacts[0].Path = "/user-mount/some-path"
	assert.False(t, we.isBaseImagePath("/user-mount"))
	assert.False(t, we.isBaseImagePath("/user-mount/some-path"))
	assert.False(t, we.isBaseImagePath("/user-mount/some-path/foo"))
	assert.True(t, we.isBaseImagePath("/user-mount-coincidence"))
}

func TestDefaultParameters(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	mockRuntimeExecutor := mocks.ContainerRuntimeExecutor{}
	templateWithOutParam := wfv1.Template{
		Outputs: wfv1.Outputs{
			Parameters: []wfv1.Parameter{
				{
					Name: "my-out",
					ValueFrom: &wfv1.ValueFrom{
						Default: pointer.StringPtr("Default Value"),
						Path:    "/path",
					},
				},
			},
		},
	}
	we := WorkflowExecutor{
		PodName:            fakePodName,
		Template:           templateWithOutParam,
		ClientSet:          fakeClientset,
		Namespace:          fakeNamespace,
		PodAnnotationsPath: fakeAnnotations,
		ExecutionControl:   nil,
		RuntimeExecutor:    &mockRuntimeExecutor,
		mainContainerID:    fakeContainerID,
	}
	mockRuntimeExecutor.On("GetFileContents", fakeContainerID, "/path").Return("", fmt.Errorf("file not found"))
	err := we.SaveParameters()
	assert.NoError(t, err)
	assert.Equal(t, *we.Template.Outputs.Parameters[0].Value, "Default Value")
}

func TestDefaultParametersEmptyString(t *testing.T) {
	fakeClientset := fake.NewSimpleClientset()
	mockRuntimeExecutor := mocks.ContainerRuntimeExecutor{}
	templateWithOutParam := wfv1.Template{
		Outputs: wfv1.Outputs{
			Parameters: []wfv1.Parameter{
				{
					Name: "my-out",
					ValueFrom: &wfv1.ValueFrom{
						Default: pointer.StringPtr(""),
						Path:    "/path",
					},
				},
			},
		},
	}
	we := WorkflowExecutor{
		PodName:            fakePodName,
		Template:           templateWithOutParam,
		ClientSet:          fakeClientset,
		Namespace:          fakeNamespace,
		PodAnnotationsPath: fakeAnnotations,
		ExecutionControl:   nil,
		RuntimeExecutor:    &mockRuntimeExecutor,
		mainContainerID:    fakeContainerID,
	}
	mockRuntimeExecutor.On("GetFileContents", fakeContainerID, "/path").Return("", fmt.Errorf("file not found"))
	err := we.SaveParameters()
	assert.NoError(t, err)
	assert.Equal(t, "", *we.Template.Outputs.Parameters[0].Value)
}

func TestIsTarball(t *testing.T) {
	tests := []struct {
		path      string
		isTarball bool
		expectErr bool
	}{
		{"testdata/file", false, false},
		{"testdata/file.zip", false, false},
		{"testdata/file.tar", false, false},
		{"testdata/file.gz", false, false},
		{"testdata/file.tar.gz", true, false},
		{"testdata/file.tgz", true, false},
		{"testdata/not-found", false, true},
	}

	for _, test := range tests {
		ok, err := isTarball(test.path)
		if test.expectErr {
			assert.Error(t, err, test.path)
		} else {
			assert.NoError(t, err, test.path)
		}
		assert.Equal(t, test.isTarball, ok, test.path)
	}
}

func TestUnzip(t *testing.T) {
	zipPath := "testdata/file.zip"
	destPath := "testdata/unzippedFile"

	// test
	err := unzip(zipPath, destPath)
	assert.NoError(t, err)

	// check unzipped file
	fileInfo, err := os.Stat(destPath)
	assert.NoError(t, err)
	assert.True(t, fileInfo.Mode().IsRegular())

	// cleanup
	err = os.Remove(destPath)
	assert.NoError(t, err)
}

func TestUntar(t *testing.T) {
	tarPath := "testdata/file.tar.gz"
	destPath := "testdata/untarredFile"

	// test
	err := untar(tarPath, destPath)
	assert.NoError(t, err)

	// check untarred file
	fileInfo, err := os.Stat(destPath)
	assert.NoError(t, err)
	assert.True(t, fileInfo.Mode().IsRegular())

	// cleanup
	err = os.Remove(destPath)
	assert.NoError(t, err)
}

func TestChmod(t *testing.T) {

	type perm struct {
		dir  string
		file string
	}

	tests := []struct {
		mode        int32
		recurse     bool
		permissions perm
	}{
		{
			0777,
			false,
			perm{
				"drwxrwxrwx",
				"-rw-------",
			},
		},
		{
			0777,
			true,
			perm{
				"drwxrwxrwx",
				"-rwxrwxrwx",
			},
		},
	}

	for _, test := range tests {
		// Setup directory and file for testing
		tempDir, err := ioutil.TempDir("testdata", "chmod-dir-test")
		assert.NoError(t, err)

		tempFile, err := ioutil.TempFile(tempDir, "chmod-file-test")
		assert.NoError(t, err)

		// TearDown test by removing directory and file
		defer os.RemoveAll(tempDir)

		// Run chmod function
		err = chmod(tempDir, test.mode, test.recurse)
		assert.NoError(t, err)

		// Check directory mode if set
		dirPermission, err := os.Stat(tempDir)
		assert.NoError(t, err)
		assert.Equal(t, dirPermission.Mode().String(), test.permissions.dir)

		// Check file mode mode if set
		filePermission, err := os.Stat(tempFile.Name())
		assert.NoError(t, err)
		assert.Equal(t, filePermission.Mode().String(), test.permissions.file)
	}

}
