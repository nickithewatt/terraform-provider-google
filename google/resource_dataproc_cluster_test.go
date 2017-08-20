package google

import (
	"fmt"
	"net/http"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"testing"

	"github.com/hashicorp/terraform/helper/acctest"
	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/terraform"

	"google.golang.org/api/dataproc/v1"
	"google.golang.org/api/googleapi"
)

const emptyTFDefinition = `
# empty def
`

func TestExtractLastResourceFromUri_withUrl(t *testing.T) {
	actual := extractLastResourceFromUri("http://something.com/one/two/three")
	expected := "three"
	if actual != expected {
		t.Fatalf("Expected %s, but got %s", expected, actual)
	}
}

func TestExtractLastResourceFromUri_WithStaticValue(t *testing.T) {
	actual := extractLastResourceFromUri("three")
	expected := "three"
	if actual != expected {
		t.Fatalf("Expected %s, but got %s", expected, actual)
	}
}

func TestExtractInitTimeout(t *testing.T) {
	actual, err := extractInitTimeout("500s")
	expected := 500
	if err != nil {
		t.Fatalf("Expected %d, but got error %v", expected, err)
	}
	if actual != expected {
		t.Fatalf("Expected %d, but got %d", expected, actual)
	}
}

func TestExtractInitTimeout_nonSeconds(t *testing.T) {
	actual, err := extractInitTimeout("5m")
	expected := 300
	if err != nil {
		t.Fatalf("Expected %d, but got error %v", expected, err)
	}
	if actual != expected {
		t.Fatalf("Expected %d, but got %d", expected, actual)
	}
}

func TestExtractInitTimeout_empty(t *testing.T) {
	_, err := extractInitTimeout("")
	expected := "time: invalid duration"
	if err != nil && err.Error() != expected {
		return
	}
	t.Fatalf("Expected an error with message '%s', but got %v", expected, err.Error())
}

func TestAccDataprocCluster_missingZoneGlobalRegion(t *testing.T) {
	rnd := acctest.RandString(10)
	resource.Test(t, resource.TestCase{
		PreCheck:  func() { testAccPreCheck(t) },
		Providers: testAccProviders,
		Steps: []resource.TestStep{
			{
				Config:      testAccCheckDataproc_missingZoneGlobalRegion(rnd),
				ExpectError: regexp.MustCompile("zone is mandatory when region is set to 'global'"),
			},
		},
	})
}

func TestAccDataprocCluster_basic(t *testing.T) {
	var cluster dataproc.Cluster
	rnd := acctest.RandString(10)
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_basic(rnd),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.basic", &cluster),

					// Default behaviour is for Dataproc to autogen or autodiscover a config bucket
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.bucket"),

					// Expect 1 master with computed values
					resource.TestCheckResourceAttr("google_dataproc_cluster.basic", "config_cluster.0.master_config.#", "1"),
					resource.TestCheckResourceAttr("google_dataproc_cluster.basic", "config_cluster.0.master_config.0.num_instances", "1"),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.master_config.0.boot_disk_size_gb"),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.master_config.0.num_local_ssds"),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.master_config.0.machine_type"),

					// Expect 2 workers with computed values
					resource.TestCheckResourceAttr("google_dataproc_cluster.basic", "config_cluster.0.worker_config.#", "1"),
					resource.TestCheckResourceAttr("google_dataproc_cluster.basic", "config_cluster.0.worker_config.0.num_instances", "2"),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.worker_config.0.boot_disk_size_gb"),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.worker_config.0.num_local_ssds"),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.worker_config.0.machine_type"),
				),
			},
		},
	})
}

func TestAccDataprocCluster_basicWithAutogenDeleteTrue(t *testing.T) {
	var cluster dataproc.Cluster
	rnd := acctest.RandString(10)
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(true),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_basicWithAutogenDeleteTrue(rnd),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.basic", &cluster),
					resource.TestCheckResourceAttrSet("google_dataproc_cluster.basic", "config_cluster.0.bucket"),
				),
			},
			{
				// Force an explicit destroy
				Config: emptyTFDefinition,
				Check:  testAccCheckDataprocAutogenBucketDeleted(&cluster),
			},
		},
	})
}

func TestAccDataprocCluster_singleNodeCluster(t *testing.T) {
	rnd := acctest.RandString(10)
	var cluster dataproc.Cluster
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_singleNodeCluster(rnd),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.single_node_cluster", &cluster),
					resource.TestCheckResourceAttr("google_dataproc_cluster.single_node_cluster", "config_cluster.0.master_config.0.num_instances", "1"),
					resource.TestCheckResourceAttr("google_dataproc_cluster.single_node_cluster", "config_cluster.0.worker_config.0.num_instances", "0"),
				),
			},
		},
	})
}

func TestAccDataprocCluster_withStagingBucket(t *testing.T) {
	rnd := acctest.RandString(10)
	var cluster dataproc.Cluster
	clusterName := fmt.Sprintf("dproc-cluster-test-%s", rnd)
	bucketName := fmt.Sprintf("%s-bucket", clusterName)

	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_withStagingBucketAndCluster(clusterName, bucketName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.with_bucket", &cluster),
					resource.TestCheckResourceAttr("google_dataproc_cluster.with_bucket", "config_cluster.0.staging_bucket", bucketName),
					resource.TestCheckResourceAttr("google_dataproc_cluster.with_bucket", "config_cluster.0.bucket", bucketName)),
			},
			{
				// Simulate destroy of cluster by removing it from definition,
				// but leaving the storage bucket (should not be auto deleted)
				Config: testAccDataprocCluster_withStagingBucketOnly(bucketName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocStagingBucketExists(bucketName),
				),
			},
		},
	})
}

func TestAccDataprocCluster_withInitAction(t *testing.T) {
	rnd := acctest.RandString(10)
	var cluster dataproc.Cluster
	bucketName := fmt.Sprintf("dproc-cluster-test-%s-init-bucket", rnd)
	objectName := "msg.txt"
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_withInitAction(rnd, bucketName, objectName),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.with_init_action", &cluster),
					resource.TestCheckResourceAttr("google_dataproc_cluster.with_init_action", "config_cluster.0.initialization_action.0.timeout_sec", "500"),
					testAccCheckDataprocClusterInitActionSucceeded(bucketName, objectName),
				),
			},
		},
	})
}

func TestAccDataprocCluster_withConfigOverrides(t *testing.T) {
	rnd := acctest.RandString(10)
	var cluster dataproc.Cluster
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_withConfigOverrides(rnd),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.with_config_overrides", &cluster),

					resource.TestCheckResourceAttr("google_dataproc_cluster.with_config_overrides", "config_cluster.0.master_config.#", "1"),
					resource.TestCheckResourceAttr("google_dataproc_cluster.with_config_overrides", "config_cluster.0.worker_config.#", "1"),

					validateDataprocCluster_withConfigOverrides("google_dataproc_cluster.with_config_overrides", &cluster),
				),
			},
		},
	})
}

func TestAccDataprocCluster_withServiceAcc(t *testing.T) {

	saEmail := os.Getenv("GOOGLE_SERVICE_ACCOUNT")
	var cluster dataproc.Cluster
	rnd := acctest.RandString(10)
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheckWithServiceAccount(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_withServiceAcc(saEmail, rnd),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists(
						"google_dataproc_cluster.with_service_account", &cluster),
					testAccCheckDataprocClusterHasServiceScopes(t, &cluster,
						"https://www.googleapis.com/auth/cloud.useraccounts.readonly",
						"https://www.googleapis.com/auth/devstorage.read_write",
						"https://www.googleapis.com/auth/logging.write",
						"https://www.googleapis.com/auth/monitoring",
					),
					resource.TestCheckResourceAttr("google_dataproc_cluster.with_service_account", "config_cluster.0.gce_config.0.service_account", saEmail),
				),
			},
		},
	})
}

func TestAccDataprocCluster_withImageVersion(t *testing.T) {
	rnd := acctest.RandString(10)
	var cluster dataproc.Cluster
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_withImageVersion(rnd),
				Check: resource.ComposeTestCheckFunc(
					testAccCheckDataprocClusterExists("google_dataproc_cluster.with_image_version", &cluster),
					resource.TestCheckResourceAttr("google_dataproc_cluster.with_image_version", "config_cluster.0.software_config.0.image_version", "preview"),
				),
			},
		},
	})
}

func TestAccDataprocCluster_withNetworkRefs(t *testing.T) {
	var c1, c2 dataproc.Cluster
	rnd := acctest.RandString(10)
	netName := fmt.Sprintf(`dproc-cluster-test-%s-net`, rnd)
	resource.Test(t, resource.TestCase{
		PreCheck:     func() { testAccPreCheck(t) },
		Providers:    testAccProviders,
		CheckDestroy: testAccCheckDataprocClusterDestroy(false),
		Steps: []resource.TestStep{
			{
				Config: testAccDataprocCluster_withNetworkRefs(rnd, netName),
				Check: resource.ComposeTestCheckFunc(
					// successful creation of the clusters is good enough to assess it worked
					testAccCheckDataprocClusterExists("google_dataproc_cluster.with_net_ref_by_url", &c1),
					testAccCheckDataprocClusterExists("google_dataproc_cluster.with_net_ref_by_name", &c2),
				),
			},
		},
	})
}

func testAccCheckDataprocClusterDestroy(expectedBucketDestroy bool) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		config := testAccProvider.Meta().(*Config)

		for _, rs := range s.RootModule().Resources {
			if rs.Type != "google_dataproc_cluster" {
				continue
			}

			if rs.Primary.ID == "" {
				return fmt.Errorf("Unable to verify delete of dataproc cluster, ID is empty")
			}

			attributes := rs.Primary.Attributes
			computedBucket := attributes["config_cluster.0.bucket"]

			// 1. Verify actual cluster deleted
			if err := validateClusterDeleted(config.Project, attributes["region"], rs.Primary.ID, config); err != nil {
				return err
			}

			// 2. Depending on delete_autogen_bucket setting, check if
			//    autogen bucket is deleted
			if expectedBucketDestroy {
				return validateBucketDoesNotExist(computedBucket, config)
			}

			// 3. Many of the tests use the default delete_autogen_bucket setting (false)
			//    Clean up to avoid dangling resources after test.
			if err := emptyAndDeleteStorageBucket(config, computedBucket); err != nil {
				return fmt.Errorf("Error occured trying to clean up autogenerate bucket after test %v", err)
			}
		}

		return nil
	}
}

func testAccCheckDataprocClusterHasServiceScopes(t *testing.T, cluster *dataproc.Cluster, scopes ...string) func(s *terraform.State) error {
	return func(s *terraform.State) error {

		if !reflect.DeepEqual(scopes, cluster.Config.GceClusterConfig.ServiceAccountScopes) {
			return fmt.Errorf("Cluster does not contain expected set of service account scopes : %v : instead %v",
				scopes, cluster.Config.GceClusterConfig.ServiceAccountScopes)
		}
		return nil
	}
}

func validateClusterDeleted(project, region, clusterName string, config *Config) error {
	_, err := config.clientDataproc.Projects.Regions.Clusters.Get(
		project, region, clusterName).Do()

	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == http.StatusNotFound {
			return nil
		} else if ok {
			return fmt.Errorf("Error verifying dataproc cluster deletion. Code: %d. Message: %s", gerr.Code, gerr.Message)
		}
		return fmt.Errorf("Error verifying dataproc cluster deletion. %s", err.Error())
	}
	return fmt.Errorf("Dataproc cluster still exists")
}

func validateBucketDoesNotExist(bucket string, config *Config) error {
	_, err := config.clientStorage.Buckets.Get(bucket).Do()

	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == http.StatusNotFound {
			return nil
		} else if ok {
			return fmt.Errorf("Error make GCP platform call to verify if bucket deleted: http code error : %d, http message error: %s", gerr.Code, gerr.Message)
		}
		return fmt.Errorf("Error make GCP platform call to verify if bucket deleted: %s", err.Error())
	}
	return fmt.Errorf("bucket still exists")
}

func validateBucketExists(bucket string, config *Config) (bool, error) {
	_, err := config.clientStorage.Buckets.Get(bucket).Do()

	if err != nil {
		if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == http.StatusNotFound {
			return false, nil
		} else if ok {
			return false, fmt.Errorf("Error make GCP platform call to verify if bucket deleted: http code error : %d, http message error: %s", gerr.Code, gerr.Message)
		}
		return false, fmt.Errorf("Error make GCP platform call to verify if bucket deleted: %s", err.Error())
	}
	return true, nil
}

func testAccCheckDataprocStagingBucketExists(bucketName string) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		config := testAccProvider.Meta().(*Config)

		exists, err := validateBucketExists(bucketName, config)
		if err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("Staging Bucket %s does not exist", bucketName)
		}
		return nil
	}

}

func testAccCheckDataprocAutogenBucketDeleted(cluster *dataproc.Cluster) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		config := testAccProvider.Meta().(*Config)
		return validateBucketDoesNotExist(cluster.Config.ConfigBucket, config)
	}
}

func testAccCheckDataprocClusterInitActionSucceeded(bucket, object string) resource.TestCheckFunc {

	// The init script will have created an object in the specified bucket.
	// Ensure it exists
	return func(s *terraform.State) error {
		config := testAccProvider.Meta().(*Config)
		_, err := config.clientStorage.Objects.Get(bucket, object).Do()
		if err != nil {
			return fmt.Errorf("Unable to verify init action success: Error reading object %s in bucket %s: %v", object, bucket, err)
		}

		return nil
	}
}

func testAccPreCheckWithServiceAccount(t *testing.T) {
	testAccPreCheck(t)
	if v := os.Getenv("GOOGLE_SERVICE_ACCOUNT"); v == "" {
		t.Skipf("GOOGLE_SERVICE_ACCOUNT must be set for the dataproc acceptance test testing service account functionality")
	}

}

func validateDataprocCluster_withConfigOverrides(n string, cluster *dataproc.Cluster) resource.TestCheckFunc {
	return func(s *terraform.State) error {

		type tfAndGCPTestField struct {
			tfAttr       string
			expectedVal  string
			actualGCPVal string
		}

		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Terraform resource Not found: %s", n)
		}

		if cluster.Config.MasterConfig == nil || cluster.Config.WorkerConfig == nil || cluster.Config.SecondaryWorkerConfig == nil {
			return fmt.Errorf("Master/Worker/SecondaryConfig values not set in GCP, expecting values")
		}

		clusterTests := []tfAndGCPTestField{
			{"config_cluster.0.master_config.0.num_instances", "3", strconv.Itoa(int(cluster.Config.MasterConfig.NumInstances))},
			{"config_cluster.0.master_config.0.boot_disk_size_gb", "10", strconv.Itoa(int(cluster.Config.MasterConfig.DiskConfig.BootDiskSizeGb))},
			{"config_cluster.0.master_config.0.num_local_ssds", "0", strconv.Itoa(int(cluster.Config.MasterConfig.DiskConfig.NumLocalSsds))},
			{"config_cluster.0.master_config.0.machine_type", "n1-standard-1", extractLastResourceFromUri(cluster.Config.MasterConfig.MachineTypeUri)},

			{"config_cluster.0.worker_config.0.num_instances", "3", strconv.Itoa(int(cluster.Config.WorkerConfig.NumInstances))},
			{"config_cluster.0.worker_config.0.boot_disk_size_gb", "11", strconv.Itoa(int(cluster.Config.WorkerConfig.DiskConfig.BootDiskSizeGb))},
			{"config_cluster.0.worker_config.0.num_local_ssds", "0", strconv.Itoa(int(cluster.Config.WorkerConfig.DiskConfig.NumLocalSsds))},
			{"config_cluster.0.worker_config.0.machine_type", "n1-standard-1", extractLastResourceFromUri(cluster.Config.WorkerConfig.MachineTypeUri)},

			{"config_cluster.0.preemptible_worker_config.0.num_instances", "1", strconv.Itoa(int(cluster.Config.SecondaryWorkerConfig.NumInstances))},
			{"config_cluster.0.preemptible_worker_config.0.boot_disk_size_gb", "12", strconv.Itoa(int(cluster.Config.SecondaryWorkerConfig.DiskConfig.BootDiskSizeGb))},
		}

		for _, attrs := range clusterTests {
			tfVal := rs.Primary.Attributes[attrs.tfAttr]
			if tfVal != attrs.expectedVal {
				return fmt.Errorf("%s: Terraform Attribute value '%s' is not as expected '%s' ", attrs.tfAttr, tfVal, attrs.expectedVal)
			}
			if attrs.actualGCPVal != tfVal {
				return fmt.Errorf("%s: Terraform Attribute value '%s' is not aligned with that in GCP '%s' ", attrs.tfAttr, tfVal, attrs.actualGCPVal)
			}
		}

		return nil
	}
}

func testAccCheckDataprocClusterExists(n string, cluster *dataproc.Cluster) resource.TestCheckFunc {
	return func(s *terraform.State) error {
		rs, ok := s.RootModule().Resources[n]
		if !ok {
			return fmt.Errorf("Terraform resource Not found: %s", n)
		}

		if rs.Primary.ID == "" {
			return fmt.Errorf("No ID is set for Dataproc cluster")
		}

		config := testAccProvider.Meta().(*Config)
		found, err := config.clientDataproc.Projects.Regions.Clusters.Get(
			config.Project, rs.Primary.Attributes["region"], rs.Primary.ID).Do()
		if err != nil {
			return err
		}

		if found.ClusterName != rs.Primary.ID {
			return fmt.Errorf("Dataproc cluster %s not found, found %s instead", rs.Primary.ID, cluster.ClusterName)
		}

		*cluster = *found

		return nil
	}
}

func testAccCheckDataproc_missingZoneGlobalRegion(rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "basic" {
	name                  = "dproc-cluster-test-%s"
	region                = "global"
}
`, rnd)
}

func testAccDataprocCluster_basic(rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "basic" {
	name                  = "dproc-cluster-test-%s"
	region                = "us-central1"
}
`, rnd)
}

func testAccDataprocCluster_basicWithAutogenDeleteTrue(rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "basic" {
	name                  = "dproc-cluster-test-%s"
	region                = "us-central1"

	cluster_config {
		delete_autogen_bucket = true
	}
}
`, rnd)
}

func testAccDataprocCluster_singleNodeCluster(rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "single_node_cluster" {
	name   = "dproc-cluster-test-%s"
	region = "us-central1"

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}

		# Because of current restrictions with computed AND default
		# [list|Set] properties, we need to add this empty config
		# here otherwise if you plan straight away afterwards you
		# will get a diff. If you have actual config values that is
		# fine, but if you were hoping to use the defaults, this is
		# required
		master_config { }
		worker_config { }
	}
}
`, rnd)
}

func testAccDataprocCluster_withConfigOverrides(rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "with_config_overrides" {
	name   = "dproc-cluster-test-%s"
	region = "us-central1"

	cluster_config {

		master_config {
			num_instances     = 3
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 10
			num_local_ssds    = 0
		}

		worker_config {
			num_instances     = 3
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 11
			num_local_ssds    = 0
		}

		preemptible_worker_config {
			num_instances     = 1
			boot_disk_size_gb = 12
		}
	}
}`, rnd)
}

func testAccDataprocCluster_withInitAction(rnd, bucket, objName string) string {
	return fmt.Sprintf(`
resource "google_storage_bucket" "init_bucket" {
	name          = "%s"
	force_destroy = "true"
}

resource "google_storage_bucket_object" "init_script" {
	name           = "dproc-cluster-test-%s-init-script.sh"
	bucket         = "${google_storage_bucket.init_bucket.name}"
	content        = <<EOL
#!/bin/bash
echo "init action success" >> /tmp/%s
gsutil cp /tmp/%s ${google_storage_bucket.init_bucket.url}
EOL

}

resource "google_dataproc_cluster" "with_init_action" {
	name   = "dproc-cluster-test-%s"
	region = "us-central1"

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}

		worker_config { }
		master_config {
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 10
		}

		initialization_action {
			script      = "${google_storage_bucket.init_bucket.url}/${google_storage_bucket_object.init_script.name}"
			timeout_sec = 500
		}
		initialization_action {
			script      = "${google_storage_bucket.init_bucket.url}/${google_storage_bucket_object.init_script.name}"
		}
	}
}`, bucket, rnd, objName, objName, rnd)
}

func testAccDataprocCluster_withStagingBucketOnly(bucketName string) string {
	return fmt.Sprintf(`
resource "google_storage_bucket" "bucket" {
	name          = "%s"
	force_destroy = "true"
}`, bucketName)
}

func testAccDataprocCluster_withStagingBucketAndCluster(clusterName, bucketName string) string {
	return fmt.Sprintf(`
%s

resource "google_dataproc_cluster" "with_bucket" {
	name   = "%s"
	region = "us-central1"
	staging_bucket = "${google_storage_bucket.bucket.name}"

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}

		worker_config { }
		master_config {
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 10
		}
	}
}`, testAccDataprocCluster_withStagingBucketOnly(bucketName), clusterName)
}

func testAccDataprocCluster_withImageVersion(rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "with_image_version" {
	name   = "dproc-cluster-test-%s"
	region = "us-central1"

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}
	}
}`, rnd)
}

func testAccDataprocCluster_withServiceAcc(saEmail string, rnd string) string {
	return fmt.Sprintf(`
resource "google_dataproc_cluster" "with_service_account" {
	name   = "dproc-cluster-test-%s"
	region = "us-central1"

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}

		worker_config { }
		master_config {
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 10
		}

		gce_config {
			service_account = "%s"
			service_account_scopes = [
				#	The following scopes necessary for the cluster to function properly are
				#	always added, even if not explicitly specified:
				#		useraccounts-ro: https://www.googleapis.com/auth/cloud.useraccounts.readonly
				#		storage-rw:      https://www.googleapis.com/auth/devstorage.read_write
				#		logging-write:   https://www.googleapis.com/auth/logging.write
				"useraccounts-ro","storage-rw","logging-write",

				#	Note for now must be in alpha order of fully qualified scope name)
				"https://www.googleapis.com/auth/monitoring"
			]
		}
	}

}`, rnd, saEmail)
}

func testAccDataprocCluster_withNetworkRefs(rnd, netName string) string {
	return fmt.Sprintf(`
resource "google_compute_network" "dataproc_network" {
	name = "%s"
	auto_create_subnetworks = true
}

#
# The default network within GCP already comes pre configured with
# certain firewall rules open to allow internal communication. As we
# are creating a new one here for this test, we need to additionally
# open up similar rules to allow the nodes to talk to each other
# internally as part of their configuration or this will just hang.
#
resource "google_compute_firewall" "dataproc_network_firewall" {
	name = "dproc-cluster-test-%s-allow-internal"
	description = "Firewall rules for dataproc Terraform acceptance testing"
	network = "${google_compute_network.dataproc_network.name}"

	allow {
		protocol = "icmp"
	}

	allow {
		protocol = "tcp"
		ports    = ["0-65535"]
	}

	allow {
		protocol = "udp"
		ports    = ["0-65535"]
	}
}

resource "google_dataproc_cluster" "with_net_ref_by_name" {
	name   = "dproc-cluster-test-%s-name"
	region = "us-central1"
	depends_on = ["google_compute_firewall.dataproc_network_firewall"]

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}

		worker_config { }
		master_config {
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 10
		}

		gce_config {
			network = "${google_compute_network.dataproc_network.name}"
		}
	}
}

resource "google_dataproc_cluster" "with_net_ref_by_url" {
	name   = "dproc-cluster-test-%s-url"
	region = "us-central1"
	depends_on = ["google_compute_firewall.dataproc_network_firewall"]

	cluster_config {
		# Keep the costs down with smallest config we can get away with
		software_config {
			properties = {
				"dataproc:dataproc.allow.zero.workers" = "true"
			}
		}
		worker_config { }
		master_config {
			machine_type      = "n1-standard-1"
			boot_disk_size_gb = 10
		}

		gce_config {
			network = "${google_compute_network.dataproc_network.self_link}"
		}
	}
}

`, netName, rnd, rnd, rnd)
}
