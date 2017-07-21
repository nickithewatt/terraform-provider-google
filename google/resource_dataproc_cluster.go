package google

import (
	"fmt"

	"github.com/hashicorp/terraform/helper/schema"

	"google.golang.org/api/dataproc/v1"

	"errors"
	"log"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
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

		SchemaVersion: 1,

		Schema: map[string]*schema.Schema{
			"name": {
				Type:        schema.TypeString,
				Description: "The name of this cluster",
				Required:    true,
				ForceNew:    true,
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
			"bucket": {
				Type:        schema.TypeString,
				Description: "The Google Cloud Storage bucket to use with the Google Cloud Storage connector",
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
			},

			"image_version": {
				Type:        schema.TypeString,
				Description: "The image version to use for the cluster",
				Optional:    true,
				Computed:    true,
				ForceNew:    true,
			},

			"network": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				ForceNew:      true,
				ConflictsWith: []string{"subnetwork"},
			},

			"subnetwork": {
				Type:          schema.TypeString,
				Optional:      true,
				Computed:      true,
				ForceNew:      true,
				ConflictsWith: []string{"network"},
			},

			"region": {
				Type:     schema.TypeString,
				Optional: true,
				Default:  "global",
				ForceNew: true,
			},

			"zone": {
				Type:     schema.TypeString,
				Required: true,
				ForceNew: true,
			},

			"labels": {
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: false,
				Elem:     schema.TypeString,
			},

			"tags": {
				Type:     schema.TypeList,
				Optional: true,
				ForceNew: true,
				Elem:     &schema.Schema{Type: schema.TypeString},
			},

			"master_config": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				ForceNew: true,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"num_masters": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
							ForceNew: false,
						},

						"machine_type": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},

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

			"worker_config": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: true,
				ForceNew: false,
				MaxItems: 1,
				Elem: &schema.Resource{
					Schema: map[string]*schema.Schema{
						"num_workers": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
							ForceNew: false,
						},

						"machine_type": {
							Type:     schema.TypeString,
							Optional: true,
							Computed: true,
							ForceNew: true,
						},

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

						"preemptible_num_workers": {
							Type:     schema.TypeInt,
							Optional: true,
							Computed: true,
							ForceNew: false,
						},

						// "preemptible_machine_type" cannot be specified directly, it takes its
						// value from the standard worker "machine_type" field

						"preemptible_boot_disk_size_gb": {
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

			"service_account": {
				Type:     schema.TypeString,
				Optional: true,
				Computed: true,
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

			"initialization_action_timeout_sec": {
				Type:     schema.TypeInt,
				Optional: true,
				Computed: true,
				ForceNew: true,
			},

			"initialization_actions": {
				Type:     schema.TypeList,
				Optional: true,
				Computed: false,
				ForceNew: true,
				Elem: &schema.Schema{
					Type: schema.TypeString,
				},
			},

			"metadata": {
				Type:     schema.TypeMap,
				Optional: true,
				ForceNew: true,
				Elem:     schema.TypeString,
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

	timeoutInMinutes := int(d.Timeout(schema.TimeoutCreate).Minutes())

	// Mandatory
	clusterName := d.Get("name").(string)
	region := d.Get("region").(string)
	zone := d.Get("zone").(string)

	gceConfig := &dataproc.GceClusterConfig{}

	if _, ok := d.GetOk("network"); ok {
		network, err := getNetworkName(d, "network")
		if err != nil {
			return err
		}
		gceConfig.NetworkUri = network
	}

	if v, ok := d.GetOk("subnetwork"); ok {
		gceConfig.SubnetworkUri = extractLastResourceFromUri(v.(string))
	}

	if v, ok := d.GetOk("tags"); ok {
		tagsList := v.([]interface{})
		tags := []string{}
		for _, v := range tagsList {
			tags = append(tags, v.(string))
		}
		gceConfig.Tags = tags
	}

	if v, ok := d.GetOk("service_account"); ok {
		gceConfig.ServiceAccount = v.(string)
	}

	if v, ok := d.GetOk("service_account_scopes"); ok {
		scopesList := v.([]interface{})
		scopes := []string{}
		for _, v := range scopesList {
			scopes = append(scopes, canonicalizeServiceScope(v.(string)))
		}

		/*
			The following scopes necessary for the cluster to function properly are
			always added, even if not explicitly specified:
				useraccounts-ro: https://www.googleapis.com/auth/cloud.useraccounts.readonly
				storage-rw:      https://www.googleapis.com/auth/devstorage.read_write
				logging-write:   https://www.googleapis.com/auth/logging.write

			We thus need to add them to the scopes as well to ensure this field is not
			picked up as changing
		*/

		sort.Strings(scopes)
		gceConfig.ServiceAccountScopes = scopes
	}

	gceConfig.ZoneUri = zone

	clusterConfig := &dataproc.ClusterConfig{
		GceClusterConfig: gceConfig,
	}

	if v, ok := d.GetOk("initialization_actions"); ok {

		timeoutInSecs := ""
		if v, ok := d.GetOk("initialization_action_timeout_sec"); ok {
			timeoutInSecs = strconv.Itoa(v.(int)) + "s"
		}

		actionList := v.([]interface{})
		actions := []*dataproc.NodeInitializationAction{}
		for _, v := range actionList {
			actions = append(actions, &dataproc.NodeInitializationAction{
				ExecutableFile:   v.(string),
				ExecutionTimeout: timeoutInSecs,
			})
		}
		clusterConfig.InitializationActions = actions
	}

	if v, ok := d.GetOk("bucket"); ok {
		clusterConfig.ConfigBucket = v.(string)
	}

	if v, ok := d.GetOk("master_config"); ok {
		masterConfigs := v.([]interface{})
		if len(masterConfigs) > 1 {
			return fmt.Errorf("Cannot specify more than one master_config.")
		}
		masterConfig := masterConfigs[0].(map[string]interface{})

		clusterConfig.MasterConfig = &dataproc.InstanceGroupConfig{
			DiskConfig: &dataproc.DiskConfig{},
		}

		if v, ok = masterConfig["num_masters"]; ok {
			clusterConfig.MasterConfig.NumInstances = int64(v.(int))
		}
		if v, ok = masterConfig["machine_type"]; ok {
			clusterConfig.MasterConfig.MachineTypeUri = extractLastResourceFromUri(v.(string))
		}
		if v, ok = masterConfig["boot_disk_size_gb"]; ok {
			clusterConfig.MasterConfig.DiskConfig.BootDiskSizeGb = int64(v.(int))
		}
		if v, ok = masterConfig["num_local_ssds"]; ok {
			clusterConfig.MasterConfig.DiskConfig.NumLocalSsds = int64(v.(int))
		}
	}

	if v, ok := d.GetOk("worker_config"); ok {
		configs := v.([]interface{})
		if len(configs) > 1 {
			return fmt.Errorf("Cannot specify more than one worker_config.")
		}
		config := configs[0].(map[string]interface{})

		clusterConfig.WorkerConfig = &dataproc.InstanceGroupConfig{
			DiskConfig: &dataproc.DiskConfig{},
		}

		if v, ok = config["num_workers"]; ok {
			clusterConfig.WorkerConfig.NumInstances = int64(v.(int))
		}
		if v, ok = config["machine_type"]; ok {
			clusterConfig.WorkerConfig.MachineTypeUri = extractLastResourceFromUri(v.(string))
		}
		if v, ok = config["boot_disk_size_gb"]; ok {
			clusterConfig.WorkerConfig.DiskConfig.BootDiskSizeGb = int64(v.(int))
		}
		if v, ok = config["num_local_ssds"]; ok {
			clusterConfig.WorkerConfig.DiskConfig.NumLocalSsds = int64(v.(int))
		}

		clusterConfig.SecondaryWorkerConfig = &dataproc.InstanceGroupConfig{
			DiskConfig: &dataproc.DiskConfig{},
		}

		if v, ok = config["preemptible_num_workers"]; ok {
			clusterConfig.SecondaryWorkerConfig.NumInstances = int64(v.(int))
		}
		if v, ok = config["preemptible_boot_disk_size_gb"]; ok {
			clusterConfig.SecondaryWorkerConfig.DiskConfig.BootDiskSizeGb = int64(v.(int))
		}
	}

	cluster := &dataproc.Cluster{
		ClusterName: clusterName,
		ProjectId:   project,
		Config:      clusterConfig,
	}

	if v, ok := d.GetOk("image_version"); ok {
		cluster.Config.SoftwareConfig = &dataproc.SoftwareConfig{
			ImageVersion: v.(string),
		}
	}

	if v, ok := d.GetOk("labels"); ok {
		m := make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			m[k] = val.(string)
		}
		cluster.Labels = m
	}
	if v, ok := d.GetOk("metadata"); ok {
		m := make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			m[k] = val.(string)
		}
		cluster.Config.GceClusterConfig.Metadata = m
	}

	// Create the cluster
	op, err := config.clientDataproc.Projects.Regions.Clusters.Create(
		project, region, cluster).Do()
	if err != nil {
		return err
	}

	log.Println(op)
	// Wait until it's created
	waitErr := dataprocClusterOperationWait(config, op, "creating Dataproc cluster", timeoutInMinutes, 3)
	if waitErr != nil {
		// The resource didn't actually create
		d.SetId("")
		return waitErr
	}

	log.Printf("[INFO] Dataproc cluster %s has been created", clusterName)

	d.SetId(clusterName)

	e := resourceDataprocClusterRead(d, meta)
	if e != nil {
		log.Print("[INFO] Got an error reading back after creating, \n", e)
	}
	return e
}

func resourceDataprocClusterUpdate(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	// The only items which are currently able to be updated, without a
	// forceNew in place are the labels and/or the number of worker nodes in a cluster
	if !(d.HasChange("labels") ||
		d.HasChange("worker_config.0.num_workers") ||
		d.HasChange("worker_config.0.preemptible_num_workers")) {
		return errors.New("*** programmer issue - update resource called however item is not allowed to be changed - investigate ***")
	}

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
	patch := config.clientDataproc.Projects.Regions.Clusters.Patch(
		project, region, clusterName, cluster)

	updMask := ""

	if d.HasChange("labels") {

		v := d.Get("labels")
		m := make(map[string]string)
		for k, val := range v.(map[string]interface{}) {
			m[k] = val.(string)
		}
		cluster.Labels = m

		updMask = "labels"
	}

	if d.HasChange("worker_config.0.num_workers") {

		wconfigs := d.Get("worker_config").([]interface{})
		conf := wconfigs[0].(map[string]interface{})

		desiredNumWorks := conf["num_workers"].(int)
		cluster.Config.WorkerConfig = &dataproc.InstanceGroupConfig{
			NumInstances: int64(desiredNumWorks),
		}

		if len(updMask) > 0 {
			updMask = updMask + ","
		}
		updMask = updMask + "config.worker_config.num_instances"
	}

	if d.HasChange("worker_config.0.preemptible_num_workers") {

		wconfigs := d.Get("worker_config").([]interface{})
		conf := wconfigs[0].(map[string]interface{})

		desiredNumWorks := conf["preemptible_num_workers"].(int)
		cluster.Config.SecondaryWorkerConfig = &dataproc.InstanceGroupConfig{
			NumInstances: int64(desiredNumWorks),
		}

		if len(updMask) > 0 {
			updMask = updMask + ","
		}
		updMask = updMask + "config.secondary_worker_config.num_instances"

	}

	patch.UpdateMask(updMask)

	op, err := patch.Do()
	if err != nil {
		return err
	}

	// Wait until it's updated
	waitErr := dataprocClusterOperationWait(config, op, "updating Dataproc cluster ", timeoutInMinutes, 2)
	if waitErr != nil {
		return waitErr
	}

	log.Printf("[INFO] Dataproc cluster %s has been updated ", d.Id())
	return resourceDataprocClusterRead(d, meta)
}

func extractLastResourceFromUri(rUri string) string {
	rUris := strings.Split(rUri, "/")
	return rUris[len(rUris)-1]
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

	d.Set("labels", cluster.Labels)
	d.Set("name", cluster.ClusterName)
	d.Set("bucket", cluster.Config.ConfigBucket)

	extracted := false
	if len(cluster.Config.InitializationActions) > 0 {
		actions := []string{}
		for _, v := range cluster.Config.InitializationActions {
			actions = append(actions, v.ExecutableFile)

			if !extracted && len(v.ExecutionTimeout) > 0 {
				tsec, err := extractInitTimeout(v.ExecutionTimeout)
				if err != nil {
					return err
				}
				d.Set("initialization_action_timeout_sec", tsec)
				extracted = true
			}
		}
		d.Set("initialization_actions", actions)
	}

	if cluster.Config.GceClusterConfig != nil {
		d.Set("zone", extractLastResourceFromUri(cluster.Config.GceClusterConfig.ZoneUri))
		d.Set("network", extractLastResourceFromUri(cluster.Config.GceClusterConfig.NetworkUri))
		d.Set("subnet", extractLastResourceFromUri(cluster.Config.GceClusterConfig.SubnetworkUri))
		d.Set("tags", cluster.Config.GceClusterConfig.Tags)
		d.Set("metadata", cluster.Config.GceClusterConfig.Metadata)
		d.Set("service_account", cluster.Config.GceClusterConfig.ServiceAccount)
		if len(cluster.Config.GceClusterConfig.ServiceAccountScopes) > 0 {
			sort.Strings(cluster.Config.GceClusterConfig.ServiceAccountScopes)
			d.Set("service_account_scopes", cluster.Config.GceClusterConfig.ServiceAccountScopes)
		}

	}

	if cluster.Config.SoftwareConfig != nil {
		d.Set("image_version", cluster.Config.SoftwareConfig.ImageVersion)
	}

	if cluster.Config.MasterConfig != nil {
		masterConfig := []map[string]interface{}{
			{
				"num_masters":       cluster.Config.MasterConfig.NumInstances,
				"boot_disk_size_gb": cluster.Config.MasterConfig.DiskConfig.BootDiskSizeGb,
				"machine_type":      extractLastResourceFromUri(cluster.Config.MasterConfig.MachineTypeUri),
				"num_local_ssds":    cluster.Config.MasterConfig.DiskConfig.NumLocalSsds,
			},
		}
		d.Set("master_config", masterConfig)
	}

	if cluster.Config.WorkerConfig != nil {
		workerConfig := []map[string]interface{}{
			{
				"num_workers":       cluster.Config.WorkerConfig.NumInstances,
				"boot_disk_size_gb": cluster.Config.WorkerConfig.DiskConfig.BootDiskSizeGb,
				"machine_type":      extractLastResourceFromUri(cluster.Config.WorkerConfig.MachineTypeUri),
				"num_local_ssds":    cluster.Config.WorkerConfig.DiskConfig.NumLocalSsds,
			},
		}
		if cluster.Config.SecondaryWorkerConfig != nil {
			workerConfig[0]["preemptible_num_workers"] = cluster.Config.SecondaryWorkerConfig.NumInstances
			workerConfig[0]["preemptible_boot_disk_size_gb"] = cluster.Config.SecondaryWorkerConfig.DiskConfig.BootDiskSizeGb
		}

		d.Set("worker_config", workerConfig)
	}

	return nil
}

func extractInitTimeout(t string) (int, error) {
	if t == "" {
		return 0, fmt.Errorf("Cannot extract init timeout from empty string")
	}
	if t[len(t)-1:] != "s" {
		return 0, fmt.Errorf("Unexpected init timeout format expecting in seconds e.g. ZZZs, found : %s", t)
	}

	tsec, err := strconv.Atoi(t[:len(t)-1])
	if err != nil {
		return 0, fmt.Errorf("Cannot convert init timeout to int: %s", err)
	}
	return tsec, nil
}

func resourceDataprocClusterDelete(d *schema.ResourceData, meta interface{}) error {
	config := meta.(*Config)

	project, err := getProject(d, config)
	if err != nil {
		return err
	}

	region := d.Get("region").(string)
	clusterName := d.Get("name").(string)
	timeoutInMinutes := int(d.Timeout(schema.TimeoutDelete).Minutes())

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