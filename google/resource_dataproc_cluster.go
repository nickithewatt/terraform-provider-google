package google

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hashicorp/terraform/helper/resource"
	"github.com/hashicorp/terraform/helper/schema"

	"google.golang.org/api/dataproc/v1"
	"google.golang.org/api/googleapi"
)

func resourceDataprocCluster() *schema.Resource {
	return &schema.Resource{
		Create: resourceDataprocClusterCreate,
		Read:   resourceDataprocClusterRead,
		Update: resourceDataprocClusterUpdate,
		Delete: resourceDataprocClusterDelete,

		Timeouts: &schema.ResourceTimeout{
			Create: schema.DefaultTimeout(10 * time.Minute),
			Update: schema.DefaultTimeout(5 * time.Minute),
			Delete: schema.DefaultTimeout(5 * time.Minute),
		},

		Schema: map[string]*schema.Schema{
			"name": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
				ValidateFunc: func(v interface{}, k string) (ws []string, errors []error) {
					value := v.(string)

					if len(value) > 55 {
						errors = append(errors, fmt.Errorf(
							"%q cannot be longer than 55 characters", k))
					}
					if !regexp.MustCompile("^[a-z0-9-]+$").MatchString(value) {
						errors = append(errors, fmt.Errorf(
							"%q can only contain lowercase letters, numbers and hyphens", k))
					}
					if !regexp.MustCompile("^[a-z]").MatchString(value) {
						errors = append(errors, fmt.Errorf(
							"%q must start with a letter", k))
					}
					if !regexp.MustCompile("[a-z0-9]$").MatchString(value) {
						errors = append(errors, fmt.Errorf(
							"%q must end with a number or a letter", k))
					}
					return
				},
			},

			"project": {
				Type:     schema.TypeString,
				Optional: true,
				ForceNew: true,
			},

			"region": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "global",
				ForceNew: true,
			},

			"labels": {
				Type:     schema.TypeMap,
				Optional: true,
				Elem:     schema.TypeString,
				// GCP automatically adds a 'goog-dataproc-cluster-name' label
				Computed: true,
			},

			"cluster_config": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{

						"delete_autogen_bucket": {
							Type:     schema.TypeBool,
							Optional: true,
							Default:  false,
						},

						"staging_bucket": {
							Type:     schema.TypeString,
							Optional: true,
							ForceNew: true,
						},
						// If the user does not specify a staging bucket, GCP will allocate one automatically.
						// The staging_bucket field provides a way for the user to supply their own
						// staging bucket. The bucket field is purely a computed field which details
						// the definitive bucket allocated and in use (either the user supplied one via
						// staging_bucket, or the GCP generated one)
						"bucket": {
							Type:     schema.TypeString,
							Computed: true,
						},

						"gce_cluster_config": {
							Type:     schema.TypeList,
							Optional: true,
							Computed: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{

									"zone": {
										Type:     schema.TypeString,
										Optional: true,
										Computed: true,
										ForceNew: true,
									},

									"network": {
										Type:          schema.TypeString,
										Optional:      true,
										Computed:      true,
										ForceNew:      true,
										ConflictsWith: []string{"cluster_config.gce_cluster_config.subnetwork"},
										StateFunc: func(s interface{}) string {
											return extractLastResourceFromUri(s.(string))
										},
									},

									"subnetwork": {
										Type:          schema.TypeString,
										Optional:      true,
										ForceNew:      true,
										ConflictsWith: []string{"cluster_config.gce_cluster_config.network"},
										StateFunc: func(s interface{}) string {
											return extractLastResourceFromUri(s.(string))
										},
									},

									"tags": {
										Type:     schema.TypeList,
										Optional: true,
										ForceNew: true,
										Elem:     &schema.Schema{Type: schema.TypeString},
									},

									"service_account": {
										Type:     schema.TypeString,
										Optional: true,
										ForceNew: true,
									},

									"service_account_scopes": {
										Type:     schema.TypeList,
										Optional: true,
										Computed: true,
										ForceNew: true,
										Elem: &schema.Schema{
											Type: schema.TypeString,
											StateFunc: func(v interface{}) string {
												return canonicalizeServiceScope(v.(string))
											},
										},
									},
								},
							},
						},

						"master_config": instanceConfigSchema(),
						"worker_config": instanceConfigSchema(),
						// preemptible_worker_config has a slightly different config
						"preemptible_worker_config": {
							Type:     schema.TypeList,
							Optional: true,
							Computed: true,
							MaxItems: 1,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"num_instances": {
										Type:     schema.TypeInt,
										Optional: true,
										Computed: true,
									},

									// API does not honour this if set ...
									// It always uses whatever is specified for the worker_config
									// "machine_type": { ... }

									"disk_config": {
										Type:     schema.TypeList,
										Optional: true,
										Computed: true,
										MaxItems: 1,

										Elem: &schema.Resource{
											Schema: map[string]*schema.Schema{

												// API does not honour this if set ...
												// It simply ignores it completely
												// "num_local_ssds": { ... }

												"boot_disk_size_gb": {
													Type:     schema.TypeInt,
													Optional: true,
													Computed: true,
													ForceNew: true,
													ValidateFunc: func(v interface{}, k string) (ws []string, errors []error) {
														value := v.(int)

														if value < 10 {
															errors = append(errors, fmt.Errorf(
																"%q cannot be less than 10", k))
														}
														return
													},
												},
											},
										},
									},

									"instance_names": {
										Type:     schema.TypeList,
										Computed: true,
										Elem:     &schema.Schema{Type: schema.TypeString},
									},
								},
							},
						},

						"software_config": {
							Type:     schema.TypeList,
							Optional: true,
							Computed: true,
							MaxItems: 1,

							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"image_version": {
										Type:     schema.TypeString,
										Optional: true,
										Computed: true,
										ForceNew: true,
									},

									"override_properties": {
										Type:     schema.TypeMap,
										Optional: true,
										ForceNew: true,
										Elem:     schema.TypeString,
									},

									"properties": {
										Type:     schema.TypeMap,
										Computed: true,
									},
								},
							},
						},

						"initialization_action": {
							Type:     schema.TypeList,
							Optional: true,
							ForceNew: true,
							Elem: &schema.Resource{
								Schema: map[string]*schema.Schema{
									"script": {
										Type:     schema.TypeString,
										Required: true,
										ForceNew: true,
									},

									"timeout_sec": {
										Type:     schema.TypeInt,
										Optional: true,
										Default:  300,
									},
								},
							},
						},
					},
				},
			},
		},
	}
}

func instanceConfigSchema() *schema.Schema {
	return &schema.Schema{
		Type:     schema.TypeList,
		Optional: true,
		Computed: true,
		MaxItems: 1,
		Elem: &schema.Resource{
			Schema: map[string]*schema.Schema{
				"num_instances": {
					Type:     schema.TypeInt,
					Optional: true,
					Computed: true,
				},

				"machine_type": {
					Type:     schema.TypeString,
					Optional: true,
					Computed: true,
					ForceNew: true,
				},

				"disk_config": {
					Type:     schema.TypeList,
					Optional: true,
					Computed: true,
					MaxItems: 1,

					Elem: &schema.Resource{
						Schema: map[string]*schema.Schema{
							"num_local_ssds": {
								Type:     schema.TypeInt,
								Optional: true,
								Computed: true,
								ForceNew: true,
							},

							"boot_disk_size_gb": {
								Type:     schema.TypeInt,
								Optional: true,
								Computed: true,
								ForceNew: true,
								ValidateFunc: func(v interface{}, k string) (ws []string, errors []error) {
									value := v.(int)

									if value < 10 {
										errors = append(errors, fmt.Errorf(
											"%q cannot be less than 10", k))
									}
									return
								},
							},
						},
					},
				},

				"instance_names": {
					Type:     schema.TypeList,
					Computed: true,
					Elem:     &schema.Schema{Type: schema.TypeString},
				},
			},
		},
	}
}

func resourceDataprocClusterCreate(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	project, err := getProject(d, config)
	if err != nil {
		return err
	}

	clusterName := d.Get("name").(string)
	region := d.Get("region").(string)
	zok := false

	cluster := &dataproc.Cluster{
		ClusterName: clusterName,
		ProjectId:   project,
		Config: &dataproc.ClusterConfig{
			GceClusterConfig: &dataproc.GceClusterConfig{},
		},
	}

	if v, ok := d.GetOk("labels"); ok {
		m := make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			m[k] = val.(string)
		}
		cluster.Labels = m
	}

	if v, ok := d.GetOk("cluster_config"); ok {

		confs := v.([]interface{})
		if (len(confs)) > 0 {

			if v, ok := d.GetOk("cluster_config.0.staging_bucket"); ok {
				cluster.Config.ConfigBucket = v.(string)
			}

			if cfg, ok := configOptions(d, "cluster_config.0.gce_cluster_config"); ok {
				log.Println("[INFO] got gce config")
				zone, zok := cfg["zone"]
				if zok {
					cluster.Config.GceClusterConfig.ZoneUri = zone.(string)
				}
				if v, ok := cfg["network"]; ok {
					cluster.Config.GceClusterConfig.NetworkUri = extractLastResourceFromUri(v.(string))
				}
				if v, ok := cfg["subnetwork"]; ok {
					cluster.Config.GceClusterConfig.SubnetworkUri = extractLastResourceFromUri(v.(string))
				}
				if v, ok := cfg["tags"]; ok {
					cluster.Config.GceClusterConfig.Tags = convertStringArr(v.([]interface{}))
				}
				if v, ok := cfg["service_account"]; ok {
					cluster.Config.GceClusterConfig.ServiceAccount = v.(string)
				}
				if v, ok := cfg["service_account_scopes"]; ok {
					cluster.Config.GceClusterConfig.ServiceAccountScopes = convertAndMapStringArr(v.([]interface{}), canonicalizeServiceScope)
					sort.Strings(cluster.Config.GceClusterConfig.ServiceAccountScopes)
				}
			}

			if cfg, ok := configOptions(d, "cluster_config.0.software_config"); ok {
				cluster.Config.SoftwareConfig = &dataproc.SoftwareConfig{}

				if v, ok := cfg["override_properties"]; ok {
					m := make(map[string]string)
					for k, val := range v.(map[string]interface{}) {
						m[k] = val.(string)
					}
					cluster.Config.SoftwareConfig.Properties = m
				}
				if v, ok := cfg["image_version"]; ok {
					cluster.Config.SoftwareConfig.ImageVersion = v.(string)
				}
			}

			if v, ok := d.GetOk("cluster_config.0.initialization_action"); ok {
				actionList := v.([]interface{})

				actions := []*dataproc.NodeInitializationAction{}
				for _, v1 := range actionList {
					actionItem := v1.(map[string]interface{})
					action := &dataproc.NodeInitializationAction{
						ExecutableFile: actionItem["script"].(string),
					}
					if x, ok := actionItem["timeout_sec"]; ok {
						action.ExecutionTimeout = strconv.Itoa(x.(int)) + "s"
					}

					actions = append(actions, action)
				}
				cluster.Config.InitializationActions = actions
			}

			if cfg, ok := configOptions(d, "cluster_config.0.master_config"); ok {
				log.Println("[INFO] got master_config")
				cluster.Config.MasterConfig = instanceGroupConfigCreate(cfg)
			}

			if cfg, ok := configOptions(d, "cluster_config.0.worker_config"); ok {
				log.Println("[INFO] got worker config")
				cluster.Config.WorkerConfig = instanceGroupConfigCreate(cfg)
			}

			if cfg, ok := configOptions(d, "cluster_config.0.preemptible_worker_config"); ok {
				log.Println("[INFO] got preemtible worker config")
				cluster.Config.SecondaryWorkerConfig = preemptibleInstanceGroupConfigCreate(cfg)
				if cluster.Config.SecondaryWorkerConfig.NumInstances > 0 {
					cluster.Config.SecondaryWorkerConfig.IsPreemptible = true
				}
			}
		}
	}

	// Checking here caters for the case where the user does not specify cluster_config
	// at all, as well where it is simply missing from the gce_cluster_config
	if region == "global" && !zok {
		return errors.New("zone is mandatory when region is set to 'global'")
	}

	// Create the cluster
	op, err := config.clientDataproc.Projects.Regions.Clusters.Create(
		project, region, cluster).Do()
	if err != nil {
		return err
	}

	d.SetId(clusterName)

	// Wait until it's created
	timeoutInMinutes := int(d.Timeout(schema.TimeoutCreate).Minutes())
	waitErr := dataprocClusterOperationWait(config, op, "creating Dataproc cluster", timeoutInMinutes, 3)
	if waitErr != nil {
		// The resource didn't actually create
		d.SetId("")
		return waitErr
	}

	log.Printf("[INFO] Dataproc cluster %s has been created", clusterName)
	return resourceDataprocClusterRead(d, meta)

}

func preemptibleInstanceGroupConfigCreate(cfg map[string]interface{}) *dataproc.InstanceGroupConfig {
	icg := &dataproc.InstanceGroupConfig{}

	if v, ok := cfg["num_instances"]; ok {
		icg.NumInstances = int64(v.(int))
	}
	if dc, ok := cfg["disk_config"]; ok {
		d := dc.([]interface{})
		if len(d) > 0 {
			dcfg := d[0].(map[string]interface{})
			icg.DiskConfig = &dataproc.DiskConfig{}

			if v, ok := dcfg["boot_disk_size_gb"]; ok {
				icg.DiskConfig.BootDiskSizeGb = int64(v.(int))
			}
		}
	}
	return icg
}

func instanceGroupConfigCreate(cfg map[string]interface{}) *dataproc.InstanceGroupConfig {
	icg := &dataproc.InstanceGroupConfig{}

	if v, ok := cfg["num_instances"]; ok {
		icg.NumInstances = int64(v.(int))
	}
	if v, ok := cfg["machine_type"]; ok {
		icg.MachineTypeUri = extractLastResourceFromUri(v.(string))
	}

	if dc, ok := cfg["disk_config"]; ok {
		d := dc.([]interface{})
		if len(d) > 0 {
			dcfg := d[0].(map[string]interface{})
			icg.DiskConfig = &dataproc.DiskConfig{}

			if v, ok := dcfg["boot_disk_size_gb"]; ok {
				icg.DiskConfig.BootDiskSizeGb = int64(v.(int))
			}
			if v, ok := dcfg["num_local_ssds"]; ok {
				icg.DiskConfig.NumLocalSsds = int64(v.(int))
			}
		}
	}
	return icg
}

func resourceDataprocClusterUpdate(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	project, err := getProject(d, config)
	if err != nil {
		return err
	}

	region := d.Get("region").(string)
	clusterName := d.Get("name").(string)
	timeoutInMinutes := int(d.Timeout(schema.TimeoutUpdate).Minutes())

	cluster := &dataproc.Cluster{
		ClusterName: clusterName,
		ProjectId:   project,
		Config:      &dataproc.ClusterConfig{},
	}

	updMask := []string{}

	if d.HasChange("labels") {
		v := d.Get("labels")
		m := make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			m[k] = val.(string)
		}
		cluster.Labels = m

		updMask = append(updMask, "labels")
	}

	if d.HasChange("cluster_config.0.worker_config.0.num_instances") {
		wconfigs := d.Get("cluster_config.0.worker_config").([]interface{})
		conf := wconfigs[0].(map[string]interface{})

		desiredNumWorks := conf["num_instances"].(int)
		cluster.Config.WorkerConfig = &dataproc.InstanceGroupConfig{
			NumInstances: int64(desiredNumWorks),
		}

		updMask = append(updMask, "config.worker_config.num_instances")
	}

	if d.HasChange("cluster_config.0.preemptible_worker_config.0.num_instances") {
		wconfigs := d.Get("cluster_config.0.preemptible_worker_config").([]interface{})
		conf := wconfigs[0].(map[string]interface{})

		desiredNumWorks := conf["num_instances"].(int)
		cluster.Config.SecondaryWorkerConfig = &dataproc.InstanceGroupConfig{
			NumInstances: int64(desiredNumWorks),
		}

		updMask = append(updMask, "config.secondary_worker_config.num_instances")
	}

	if len(updMask) > 0 {
		patch := config.clientDataproc.Projects.Regions.Clusters.Patch(
			project, region, clusterName, cluster)
		op, err := patch.UpdateMask(strings.Join(updMask, ",")).Do()
		if err != nil {
			return err
		}

		// Wait until it's updated
		waitErr := dataprocClusterOperationWait(config, op, "updating Dataproc cluster ", timeoutInMinutes, 2)
		if waitErr != nil {
			return waitErr
		}

		log.Printf("[INFO] Dataproc cluster %s has been updated ", d.Id())
	}

	return resourceDataprocClusterRead(d, meta)
}

func resourceDataprocClusterRead(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	project, err := getProject(d, config)
	if err != nil {
		return err
	}

	region := d.Get("region").(string)
	clusterName := d.Get("name").(string)

	cluster, err := config.clientDataproc.Projects.Regions.Clusters.Get(
		project, region, clusterName).Do()
	if err != nil {
		return handleNotFoundError(err, d, fmt.Sprintf("Dataproc Cluster %q", clusterName))
	}

	d.Set("name", cluster.ClusterName)
	d.Set("region", region)
	d.Set("labels", cluster.Labels)

	cfg, err := flattenClusterConfig(d, cluster.Config)
	if err != nil {
		return err
	}

	d.Set("cluster_config", cfg)
	return nil
}

func flattenClusterConfig(d *schema.ResourceData, cfg *dataproc.ClusterConfig) ([]map[string]interface{}, error) {

	data := getOrCreateNewMapSchema(d, "cluster_config")

	data["bucket"] = cfg.ConfigBucket
	data["gce_cluster_config"] = flattenGceClusterConfig(data, cfg.GceClusterConfig)
	data["software_config"] = flattenSoftwareConfig(data, cfg.SoftwareConfig)

	data["master_config"] = flattenInstanceGroupConfig(data, "master_config", cfg.MasterConfig)
	data["worker_config"] = flattenInstanceGroupConfig(data, "worker_config", cfg.WorkerConfig)
	data["preemptible_worker_config"] = flattenPreemptibleInstanceGroupConfig(data, "preemptible_worker_config", cfg.SecondaryWorkerConfig)

	if len(cfg.InitializationActions) > 0 {
		val, err := flattenInitializationActions(cfg.InitializationActions)
		if err != nil {
			return nil, err
		}
		data["intialization_action"] = val
	}
	return []map[string]interface{}{data}, nil
}

func flattenSoftwareConfig(parent map[string]interface{}, sc *dataproc.SoftwareConfig) []map[string]interface{} {
	data := getOrCreateNewMap(parent, "software_config")
	data["image_version"] = sc.ImageVersion
	data["properties"] = sc.Properties

	return []map[string]interface{}{data}
}

func flattenInitializationActions(nia []*dataproc.NodeInitializationAction) ([]map[string]interface{}, error) {

	actions := []map[string]interface{}{}
	for _, v := range nia {
		action := map[string]interface{}{
			"script": v.ExecutableFile,
		}
		if len(v.ExecutionTimeout) > 0 {
			tsec, err := extractInitTimeout(v.ExecutionTimeout)
			if err != nil {
				return nil, err
			}
			action["timeout_sec"] = tsec
		}

		actions = append(actions, action)
	}
	return actions, nil

}

func flattenGceClusterConfig(parent map[string]interface{}, gcc *dataproc.GceClusterConfig) []map[string]interface{} {

	gceConfig := getOrCreateNewMap(parent, "gce_cluster_config")
	gceConfig["zone"] = extractLastResourceFromUri(gcc.ZoneUri)
	gceConfig["tags"] = gcc.Tags
	gceConfig["service_account"] = gcc.ServiceAccount

	if gcc.NetworkUri != "" {
		gceConfig["network"] = extractLastResourceFromUri(gcc.NetworkUri)
	}
	if gcc.SubnetworkUri != "" {
		gceConfig["subnetwork"] = extractLastResourceFromUri(gcc.SubnetworkUri)
	}

	if len(gcc.ServiceAccountScopes) > 0 {
		sort.Strings(gcc.ServiceAccountScopes)
		gceConfig["service_account_scopes"] = gcc.ServiceAccountScopes
	}
	return []map[string]interface{}{gceConfig}
}

func flattenPreemptibleInstanceGroupConfig(parent map[string]interface{}, name string, icg *dataproc.InstanceGroupConfig) []map[string]interface{} {
	data := getOrCreateNewMap(parent, name)
	disk := getOrCreateNewMap(data, "disk_config")
	data["instance_names"] = []string{}

	if icg != nil {
		data["num_instances"] = icg.NumInstances
		data["instance_names"] = icg.InstanceNames
		if icg.DiskConfig != nil {
			disk["boot_disk_size_gb"] = icg.DiskConfig.BootDiskSizeGb
		}
	}

	data["disk_config"] = []map[string]interface{}{disk}
	return []map[string]interface{}{data}
}

func flattenInstanceGroupConfig(parent map[string]interface{}, name string, icg *dataproc.InstanceGroupConfig) []map[string]interface{} {
	data := getOrCreateNewMap(parent, name)
	disk := getOrCreateNewMap(data, "disk_config")
	data["instance_names"] = []string{}

	if icg != nil {
		data["num_instances"] = icg.NumInstances
		data["machine_type"] = extractLastResourceFromUri(icg.MachineTypeUri)
		data["instance_names"] = icg.InstanceNames
		if icg.DiskConfig != nil {
			disk["boot_disk_size_gb"] = icg.DiskConfig.BootDiskSizeGb
			disk["num_local_ssds"] = icg.DiskConfig.NumLocalSsds
		}
	}

	data["disk_config"] = []map[string]interface{}{disk}
	return []map[string]interface{}{data}
}

func extractInitTimeout(t string) (int, error) {
	d, err := time.ParseDuration(t)
	if err != nil {
		return 0, err
	}
	return int(d.Seconds()), nil
}

func resourceDataprocClusterDelete(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	project, err := getProject(d, config)
	if err != nil {
		return err
	}

	region := d.Get("region").(string)
	clusterName := d.Get("name").(string)
	deleteAutoGenBucket := d.Get("cluster_config.0.delete_autogen_bucket").(bool)
	timeoutInMinutes := int(d.Timeout(schema.TimeoutDelete).Minutes())

	if deleteAutoGenBucket {
		if err := deleteAutogenBucketIfExists(d, meta); err != nil {
			return err
		}
	}

	log.Printf("[DEBUG] Deleting Dataproc cluster %s", clusterName)
	op, err := config.clientDataproc.Projects.Regions.Clusters.Delete(
		project, region, clusterName).Do()
	if err != nil {
		return err
	}

	// Wait until it's deleted
	waitErr := dataprocClusterOperationWait(config, op, "deleting Dataproc cluster", timeoutInMinutes, 3)
	if waitErr != nil {
		return waitErr
	}

	log.Printf("[INFO] Dataproc cluster %s has been deleted", d.Id())
	d.SetId("")
	return nil
}

func deleteAutogenBucketIfExists(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	// If the user did not specify a specific override staging bucket, then GCP
	// creates one automatically. Clean this up to avoid dangling resources.
	if v, ok := d.GetOk("cluster_config.0.staging_bucket"); ok {
		log.Printf("[DEBUG] staging bucket %s (for dataproc cluster) has explicitly been set, leaving it...", v)
		return nil
	}
	bucket := d.Get("cluster_config.0.bucket").(string)

	log.Printf("[DEBUG] Attempting to delete autogenerated bucket %s (for dataproc cluster)", bucket)
	return emptyAndDeleteStorageBucket(config, bucket)
}

func emptyAndDeleteStorageBucket(config *Config, bucket string) error {
	err := deleteStorageBucketContents(config, bucket)
	if err != nil {
		return err
	}

	err = deleteEmptyBucket(config, bucket)
	if err != nil {
		return err
	}
	return nil
}

func deleteEmptyBucket(config *Config, bucket string) error {
	// remove empty bucket
	err := resource.Retry(1*time.Minute, func() *resource.RetryError {
		err := config.clientStorage.Buckets.Delete(bucket).Do()
		if err == nil {
			return nil
		}
		gerr, ok := err.(*googleapi.Error)
		if gerr.Code == http.StatusNotFound {
			// Bucket may be gone already ignore
			return nil
		}
		if ok && gerr.Code == http.StatusTooManyRequests {
			return resource.RetryableError(gerr)
		}
		return resource.NonRetryableError(err)
	})
	if err != nil {
		fmt.Printf("[ERROR] Attempting to delete autogenerated bucket (for dataproc cluster): Error deleting bucket %s: %v\n\n", bucket, err)
		return err
	}
	log.Printf("[DEBUG] Attempting to delete autogenerated bucket (for dataproc cluster): Deleted bucket %v\n\n", bucket)

	return nil

}

func deleteStorageBucketContents(config *Config, bucket string) error {

	res, err := config.clientStorage.Objects.List(bucket).Do()
	if gerr, ok := err.(*googleapi.Error); ok && gerr.Code == http.StatusNotFound {
		// Bucket is already gone ...
		return nil
	}
	if err != nil {
		log.Fatalf("[DEBUG] Attempting to delete autogenerated bucket %s (for dataproc cluster). Error Objects.List failed: %v", bucket, err)
		return err
	}

	if len(res.Items) > 0 {
		// purge the bucket...
		log.Printf("[DEBUG] Attempting to delete autogenerated bucket (for dataproc cluster). \n\n")

		for _, object := range res.Items {
			log.Printf("[DEBUG] Attempting to delete autogenerated bucket (for dataproc cluster). Found %s", object.Name)

			err := config.clientStorage.Objects.Delete(bucket, object.Name).Do()
			if err != nil {
				if gerr, ok := err.(*googleapi.Error); ok && gerr.Code != http.StatusNotFound {
					log.Printf("[DEBUG] Attempting to delete autogenerated bucket (for dataproc cluster): Error trying to delete object: %s %s\n\n", object.Name, err)
					return err
				}
			}
			log.Printf("[DEBUG] Attempting to delete autogenerated bucket (for dataproc cluster): Object deleted: %s \n\n", object.Name)
		}
	}

	return nil
}

func configOptions(d *schema.ResourceData, option string) (map[string]interface{}, bool) {
	if v, ok := d.GetOk(option); ok {
		clist := v.([]interface{})
		if len(clist) == 0 {
			return nil, false
		}

		if clist[0] != nil {
			return clist[0].(map[string]interface{}), true
		}
	}
	return nil, false
}

func getOrCreateNewMapSchema(d *schema.ResourceData, key string) map[string]interface{} {
	if v, ok := d.GetOk(key); ok {
		clist := v.([]interface{})
		if len(clist) == 0 {
			return map[string]interface{}{}
		}

		if clist[0] != nil {
			return clist[0].(map[string]interface{})
		}
	}
	return map[string]interface{}{}
}

func getOrCreateNewMap(d map[string]interface{}, key string) map[string]interface{} {
	if v, ok := d[key]; ok {
		clist := v.([]interface{})
		if len(clist) == 0 {
			return map[string]interface{}{}
		}

		if clist[0] != nil {
			return clist[0].(map[string]interface{})
		}
	}
	return map[string]interface{}{}
}