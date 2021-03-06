package azureud

import (
	"os"
	"strconv"
	"strings"
	"testing"

	log "github.com/Sirupsen/logrus"
	gofig "github.com/akutz/gofig/types"
	"github.com/akutz/goof"

	"github.com/stretchr/testify/assert"

	"github.com/codedellemc/libstorage/api/context"
	"github.com/codedellemc/libstorage/api/server"
	apitests "github.com/codedellemc/libstorage/api/tests"
	"github.com/codedellemc/libstorage/api/types"
	"github.com/codedellemc/libstorage/api/utils"
	"github.com/codedellemc/libstorage/drivers/storage/azureud"
	azureUtils "github.com/codedellemc/libstorage/drivers/storage/azureud/utils"
)

// Put contents of sample config.yml here
var (
	configYAMLazure = []byte(`
azureud:
  resourceGroup: "trex"
  subscriptionID: "c971aa51-5850-460a-b300-3265d4af154b"
  tenantID: "ebbc4596-9828-453c-b95e-b8cb122f45bd"
  clientID: "5d7fbebc-2e7b-487d-bf6e-04e4bee8e8cc"
  clientSecret: "fill_your_secret"
  storageAccount: "trexdisks256"
  storageAccessKey: "fill_your_seceret"
`)
)

var volumeName string
var volumeName2 string

type CleanupIface interface {
	cleanup(key string)
}

type CleanupObjectContextT struct {
	objects map[string]CleanupIface
}

var cleanupObjectContext CleanupObjectContextT

func (ctx *CleanupObjectContextT) remove(key string) {
	delete(ctx.objects, key)
}

func (ctx *CleanupObjectContextT) cleanup() {
	for key, value := range ctx.objects {
		value.cleanup(key)
		delete(ctx.objects, key)
	}
}

type CleanupVolume struct {
	vol    *types.Volume
	client types.Client
}

func (ctx *CleanupObjectContextT) add(
	key string,
	vol *types.Volume,
	client types.Client) {

	cobj := &CleanupVolume{vol: vol, client: client}
	ctx.objects[key] = cobj
}

func (cvol *CleanupVolume) cleanup(key string) {
	cvol.client.API().VolumeDetach(nil, azureud.Name, cvol.vol.Name,
		&types.VolumeDetachRequest{Force: true})
	cvol.client.API().VolumeRemove(nil, azureud.Name, cvol.vol.Name, false)
}

// Check environment vars to see whether or not to run this test
func skipTests() bool {
	travis, _ := strconv.ParseBool(os.Getenv("TRAVIS"))
	noTestAZURE, _ := strconv.ParseBool(os.Getenv("TEST_SKIP_AZUREDISK"))
	return travis || noTestAZURE
}

// Set volume names to first part of UUID before the -
func init() {
	uuid, _ := types.NewUUID()
	uuids := strings.Split(uuid.String(), "-")
	volumeName = "test-vol-" + uuids[0]
	uuid, _ = types.NewUUID()
	uuids = strings.Split(uuid.String(), "-")
	volumeName2 = "test-vol-" + uuids[0]
	cleanupObjectContext = CleanupObjectContextT{
		objects: make(map[string]CleanupIface),
	}
}

func TestMain(m *testing.M) {
	server.CloseOnAbort()
	ec := m.Run()
	os.Exit(ec)
}

///////////////////////////////////////////////////////////////////////
/////////                    PUBLIC TESTS                     /////////
///////////////////////////////////////////////////////////////////////
// Check if InstanceID metadata is properly returned by executor
// and InstanceID.ID is filled out by InstanceInspect
func TestInstanceID(t *testing.T) {
	if skipTests() {
		t.SkipNow()
	}

	ctx := context.Background()
	// Get Instance ID from executor
	iid, err := azureUtils.InstanceID(ctx)
	assert.NoError(t, err)
	if err != nil {
		t.Fatal(err)
	}

	assert.NotEqual(t, iid.ID, "")

	// test resulting InstanceID
	apitests.Run(
		t, azureud.Name, configYAMLazure,
		(&apitests.InstanceIDTest{
			Driver:   azureud.Name,
			Expected: iid,
		}).Test)
	cleanupObjectContext.cleanup()
}

// Test if Services are configured and returned properly from the client
func TestServices(t *testing.T) {
	if skipTests() {
		t.SkipNow()
	}

	tf := func(config gofig.Config, client types.Client, t *testing.T) {
		reply, err := client.API().Services(nil)
		assert.NoError(t, err)
		assert.Equal(t, len(reply), 1)

		_, ok := reply[azureud.Name]
		assert.True(t, ok)
	}
	apitests.Run(t, azureud.Name, configYAMLazure, tf)
	cleanupObjectContext.cleanup()
}

// Test volume functionality from storage driver
func TestVolumeCreateRemove(t *testing.T) {
	if skipTests() {
		t.SkipNow()
	}

	tf := func(config gofig.Config, client types.Client, t *testing.T) {
		vol := volumeCreate(t, client, volumeName)
		volumeRemove(t, client, vol.ID)
	}
	apitests.Run(t, azureud.Name, configYAMLazure, tf)
	cleanupObjectContext.cleanup()
}

// Test volume functionality from storage driver
func TestVolumes(t *testing.T) {
	if skipTests() {
		t.SkipNow()
	}

	tf := func(config gofig.Config, client types.Client, t *testing.T) {
		_ = volumeCreate(t, client, volumeName)
		_ = volumeCreate(t, client, volumeName2)

		vol1 := volumeByName(t, client, volumeName)
		vol2 := volumeByName(t, client, volumeName2)

		volumeRemove(t, client, vol1.ID)
		volumeRemove(t, client, vol2.ID)
	}
	apitests.Run(t, azureud.Name, configYAMLazure, tf)
	cleanupObjectContext.cleanup()
}

// Test volume functionality from storage driver
func TestVolumeAttachDetach(t *testing.T) {
	if skipTests() {
		t.SkipNow()
	}
	var vol *types.Volume
	tf := func(config gofig.Config, client types.Client, t *testing.T) {
		vol = volumeCreate(t, client, volumeName)
		_ = volumeAttach(t, client, vol.ID)
		_ = volumeInspectAttached(t, client, vol.ID)
		_ = volumeInspectAttachedToMyInstance(t, client, vol.ID)
	}
	tf2 := func(config gofig.Config, client types.Client, t *testing.T) {
		_ = volumeInspectAttachedToMyInstanceWithForeignInstance(
			t, client, vol.ID)
	}
	tf3 := func(config gofig.Config, client types.Client, t *testing.T) {
		_ = volumeDetach(t, client, vol.ID)
		_ = volumeInspectDetached(t, client, vol.ID)
		volumeRemove(t, client, vol.ID)
	}
	apitests.Run(t, azureud.Name, configYAMLazure, tf)
	apitests.RunWithClientType(t, types.ControllerClient, azureud.Name,
		configYAMLazure, tf2)
	apitests.Run(t, azureud.Name, configYAMLazure, tf3)
	cleanupObjectContext.cleanup()
}

///////////////////////////////////////////////////////////////////////
/////////        PRIVATE TESTS FOR VOLUME FUNCTIONALITY       /////////
///////////////////////////////////////////////////////////////////////
// Test volume creation specifying size and volume name
func volumeCreate(
	t *testing.T, client types.Client, volumeName string) *types.Volume {
	log.WithField("volumeName", volumeName).Info("creating volume")
	// Prepare request for storage driver call to create volume
	size := int64(1)

	opts := map[string]interface{}{
		"priority": 2,
		"owner":    "root@example.com",
	}
	volumeCreateRequest := &types.VolumeCreateRequest{
		Name: volumeName,
		Size: &size,
		Opts: opts,
	}
	// Send request and retrieve created libStorage types.Volume
	vol, err := client.API().VolumeCreate(nil, azureud.Name,
		volumeCreateRequest)
	assert.NoError(t, err)
	if err != nil {
		t.FailNow()
		t.Error("failed volumeCreate")
	}
	apitests.LogAsJSON(vol, t)
	// Add obj to automated cleanup in case of errors
	cleanupObjectContext.add(vol.ID, vol, client)
	// Check volume options
	assert.Equal(t, volumeName, vol.Name)
	assert.Equal(t, size, vol.Size)
	return vol
}

// Test volume retrieval by volume name using Volumes, which retrieves all
// volumes from the storage driver without filtering, and filters the volumes
// externally.
func volumeByName(
	t *testing.T, client types.Client, volumeName string) *types.Volume {
	log.WithField("volumeName", volumeName).Info("get volume by name")
	// Retrieve all volumes
	vols, err := client.API().Volumes(nil, 0)
	assert.NoError(t, err)
	if err != nil {
		t.FailNow()
	}
	// Filter volumes to those under the azure service,
	// and find a volume matching inputted volume name
	assert.Contains(t, vols, azureud.Name)
	for _, vol := range vols[azureud.Name] {
		if vol.Name == volumeName {
			return vol
		}
	}
	// No matching volumes found
	t.FailNow()
	t.Error("failed volumeByName")
	return nil
}

// Test volume removal by volume ID
func volumeRemove(t *testing.T, client types.Client, volumeID string) {
	log.WithField("volumeID", volumeID).Info("removing volume")
	err := client.API().VolumeRemove(
		nil, azureud.Name, volumeID, false)
	assert.NoError(t, err)
	if err != nil {
		t.Error("failed volumeRemove")
		t.FailNow()
	}
	cleanupObjectContext.remove(volumeID)
}

// Test volume attachment by volume ID
func volumeAttach(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	log.WithField("volumeID", volumeID).Info("attaching volume")
	// Get next device name from executor
	nextDevice, err := client.Executor().NextDevice(
		context.Background().WithValue(
			context.ServiceKey, azureud.Name),
		utils.NewStore())
	assert.NoError(t, err)
	if err != nil {
		t.Error("error getting next device name from executor")
		t.FailNow()
	}
	reply, token, err := client.API().VolumeAttach(
		nil, azureud.Name, volumeID, &types.VolumeAttachRequest{
			NextDeviceName: &nextDevice,
		})
	assert.NoError(t, err)
	if err != nil {
		t.Error("failed volumeAttach")
		t.FailNow()
	}
	apitests.LogAsJSON(reply, t)
	assert.NotEqual(t, token, "")
	return reply
}

// Test volume retrieval by volume ID using VolumeInspect, which directly
// retrieves matching volumes from the storage driver. Contrast with
// volumeByID, which uses Volumes to retrieve all volumes from the storage
// driver without filtering, and filters the volumes externally.
func volumeInspect(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	log.WithField("volumeID", volumeID).Info("inspecting volume")
	reply, err := client.API().VolumeInspect(
		nil, azureud.Name, volumeID, 0)
	assert.NoError(t, err)
	if err != nil {
		t.Error("failed volumeInspect")
		t.FailNow()
	}
	apitests.LogAsJSON(reply, t)
	return reply
}

// Test if volume is attached, its Attachments field should be populated
func volumeInspectAttached(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	log.WithField("volumeID", volumeID).Info("inspecting volume")
	reply, err := client.API().VolumeInspect(
		nil, azureud.Name, volumeID,
		types.VolAttReq)
	assert.NoError(t, err)
	if err != nil {
		t.Error("failed volumeInspectAttached")
		t.FailNow()
	}
	apitests.LogAsJSON(reply, t)
	assert.Len(t, reply.Attachments, 1)
	return reply
}

// Test if volume is attached to specified instance
func volumeInspectAttachedToMyInstance(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	log.WithField("volumeID", volumeID).Info(
		"inspecting volume attached to my instance")
	reply, err := client.API().VolumeInspect(
		nil, azureud.Name, volumeID,
		types.VolAttReqOnlyVolsAttachedToInstance)
	assert.NoError(t, err)
	if err != nil {
		t.Error("failed volumeInspectAttached")
		t.FailNow()
	}
	apitests.LogAsJSON(reply, t)
	assert.Len(t, reply.Attachments, 1)
	return reply
}

// Test if volume is attached to my instance with foreign instance in filter
func volumeInspectAttachedToMyInstanceWithForeignInstance(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	ctx := context.Background()
	iidm := types.InstanceIDMap{"azureud": &types.InstanceID{
		ID:     "none",
		Driver: "azureud"}}
	ctx = ctx.WithValue(context.AllInstanceIDsKey, iidm)
	log.WithField("volumeID", volumeID).Info(
		"inspecting volume attached to my instance with " +
			"foreign instance in filter")
	reply, err := client.API().VolumeInspect(
		ctx, azureud.Name, volumeID,
		types.VolAttReqOnlyVolsAttachedToInstance)
	// Filtering as "to me" returns a 404 since its not attached to us
	assert.Error(t, err)
	httpErr := err.(goof.HTTPError)
	assert.Equal(t, 404, httpErr.Status())
	return reply
}

// Test if volume is detached, its Attachments field should not be populated
func volumeInspectDetached(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	log.WithField("volumeID", volumeID).Info("inspecting volume")
	reply, err := client.API().VolumeInspect(
		nil, azureud.Name, volumeID,
		types.VolAttNone)
	assert.NoError(t, err)

	if err != nil {
		t.Error("failed volumeInspectDetached")
		t.FailNow()
	}
	apitests.LogAsJSON(reply, t)
	assert.Len(t, reply.Attachments, 0)
	apitests.LogAsJSON(reply, t)
	return reply
}

// Test detaching volume by volume ID
func volumeDetach(
	t *testing.T, client types.Client, volumeID string) *types.Volume {
	log.WithField("volumeID", volumeID).Info("detaching volume")
	reply, err := client.API().VolumeDetach(
		nil, azureud.Name, volumeID, &types.VolumeDetachRequest{})
	assert.NoError(t, err)
	if err != nil {
		t.Error("failed volumeDetach")
		t.FailNow()
	}
	apitests.LogAsJSON(reply, t)
	assert.Len(t, reply.Attachments, 0)
	return reply
}
