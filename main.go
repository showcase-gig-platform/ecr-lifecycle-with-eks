package main

import (
	"context"
	"flag"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials/stscreds"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"github.com/aws/aws-sdk-go-v2/service/eks"
	"github.com/aws/aws-sdk-go-v2/service/sts"
	"gopkg.in/yaml.v3"
	"io/ioutil"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"os"
	"regexp"
	"sigs.k8s.io/aws-iam-authenticator/pkg/token"
	"sort"
	"strings"
	"time"
)

type refCluster struct {
	Region      string `yaml:"region"`
	ClusterName string `yaml:"clusterName"`
	RoleArn     string `yaml:"roleARN"`
}

type targetEcr struct {
	Region  string   `yaml:"region"`
	RoleArn string   `yaml:"roleARN"`
	Repos   []string `yaml:"repos"`
}

type lifecycle struct {
	CountType string `yaml:"type"`
	Number    int    `yaml:"number"`
}

type appConfig struct {
	DefaultRegion string       `yaml:"region"`
	AwsProfile    string       `yaml:"profile,omitempty"`
	TargetEcr     targetEcr    `yaml:"ecr"`
	RefClusters   []refCluster `yaml:"eks"`
	Lifecycle     lifecycle    `yaml:"commonLifecycle"`
	IgnoreRegex   []string     `yaml:"ignoreRegex,omitempty"`
}

func main() {
	configFile := flag.String("config-file", "/config.yaml", "Location of config file.")
	dryRun := flag.Bool("dry-run", false, "enable dry run (just log tags to be delete)")
	klog.InitFlags(nil)
	flag.Parse()

	klog.Info("start ecr-lifecycle-with-eks")

	// load and validate config
	appCfg, err := readConfig(*configFile)
	if err != nil {
		klog.Exitf("unable to load application config, %v", err)
	}
	if ok, message := validateConfig(appCfg); !ok {
		klog.Exit(message)
	}
	if appCfg.AwsProfile != "" {
		klog.Info("Override environment variable `AWS_PROFILE` with profile specified in config file.")
		os.Setenv("AWS_PROFILE", appCfg.AwsProfile)
	}
	klog.Infof("config loaded: %#v", appCfg)
	// end config

	ctx := context.Background()
	baCfg, err := baseAwsConfig(ctx, appCfg.DefaultRegion, appCfg.AwsProfile)
	if err != nil {
		klog.Exitf("unable to load AWS SDK config, %v", err)
	}

	// list container image in use from kubernetes clusters
	var pods []v1.Pod
	for _, cluster := range appCfg.RefClusters {
		c, err := kubeClient(ctx, baCfg, appCfg.DefaultRegion, cluster)
		if err != nil {
			klog.Errorf("failed to get kubernetes client, %v", err)
			continue
		}
		pl, err := c.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
		if err != nil {
			klog.Errorf("failed to list pods, %v", err)
			continue
		}
		pods = append(pods, pl.Items...)
	}
	images := listUniqueImages(pods)
	// end list container images

	ecrRegion := appCfg.DefaultRegion
	if appCfg.TargetEcr.Region != "" {
		ecrRegion = appCfg.TargetEcr.Region
	}
	aaCfg, err := assumeAwsConfig(ctx, baCfg, appCfg.TargetEcr.RoleArn, ecrRegion)
	if err != nil {
		klog.Exitf("failed to assume role, %v", err)
	}
	ecrCli := ecr.NewFromConfig(aaCfg)
	repos, err := ecrCli.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{
		RepositoryNames: appCfg.TargetEcr.Repos,
	})
	if err != nil {
		klog.Exitf("failed to describe ecr repositories, %v", err)
	}
	for _, repo := range repos.Repositories {
		inUseTags := inUseImageTags(images, *repo.RepositoryUri)
		images, err := ecrCli.DescribeImages(ctx, &ecr.DescribeImagesInput{RepositoryName: repo.RepositoryName})
		if err != nil {
			klog.Errorf("failed to describe-images in ecr repository, %v", err)
			continue
		}
		var candidateTags []string
		if appCfg.Lifecycle.CountType == "sinceImagePushed" {
			candidateTags = deletionCandidateTagsBySinceImagePushed(images.ImageDetails, appCfg.Lifecycle.Number)
		} else {
			candidateTags = deletionCandidateTagsByImageCountMoreThan(images.ImageDetails, appCfg.Lifecycle.Number)
		}

		if len(candidateTags) == 0 {
			klog.Infof("No candidate images to delete. repository: %v", *repo.RepositoryName)
			continue
		} else {
			klog.Infof("Repository: %v. Candidate tags: %v. In use tags: %v", *repo.RepositoryName, candidateTags, inUseTags)
		}

		deleteTags := decideDeleteTags(candidateTags, inUseTags, appCfg.IgnoreRegex)
		if len(deleteTags) == 0 {
			klog.Infof("No images to delete. repository: %v", *repo.RepositoryName)
			continue
		}

		if *dryRun {
			klog.Infof("dry-run enabled, images to be deleted -> Repo: %v, Tags: %v", *repo.RepositoryName, deleteTags)
			continue
		}

		err = deleteEcrImage(ctx, ecrCli, deleteTags, *repo.RepositoryName)
		if err != nil {
			klog.Errorf("failed to delete ecr images, %v", err)
		}
	}
}

func readConfig(location string) (appConfig, error) {
	var cnf appConfig
	buf, err := ioutil.ReadFile(location)
	if err != nil {
		return cnf, err
	}
	if err := yaml.Unmarshal(buf, &cnf); err != nil {
		return cnf, err
	}
	return cnf, nil
}

func validateConfig(cfg appConfig) (bool, string) {
	if cfg.Lifecycle == (lifecycle{}) {
		return false, "`commonLifecycle` must not be empty."
	}
	if !(cfg.Lifecycle.CountType == "sinceImagePushed" || cfg.Lifecycle.CountType == "imageCountMoreThan") {
		return false, "`commonLifecycle.type` must be `sinceImagePushed` or `imageCountMoreThan`."
	}
	if len(cfg.TargetEcr.Repos) == 0 {
		return false, "at least one `ecr.repos` must be specified."
	}
	return true, ""
}

func baseAwsConfig(ctx context.Context, region, profile string) (aws.Config, error) {
	return config.LoadDefaultConfig(ctx, config.WithSharedConfigProfile(profile), config.WithRegion(region))
}

func assumeAwsConfig(ctx context.Context, cfg aws.Config, role, region string) (aws.Config, error) {
	stsClient := sts.NewFromConfig(cfg)
	creds := stscreds.NewAssumeRoleProvider(stsClient, role)
	return config.LoadDefaultConfig(ctx, config.WithCredentialsProvider(aws.NewCredentialsCache(creds)), config.WithRegion(region))
}

func eksEndpoint(ctx context.Context, cfg aws.Config, clusterName string) (string, error) {
	es := eks.NewFromConfig(cfg)
	res, err := es.DescribeCluster(ctx, &eks.DescribeClusterInput{Name: aws.String(clusterName)})
	if err != nil {
		return "", err
	}
	return *res.Cluster.Endpoint, nil
}

func kubeClient(ctx context.Context, baseAwsConfig aws.Config, defaultRegion string, cluster refCluster) (*kubernetes.Clientset, error) {
	region := defaultRegion
	if cluster.Region != "" {
		region = cluster.Region
	}
	cfg, err := assumeAwsConfig(ctx, baseAwsConfig, cluster.RoleArn, region)
	if err != nil {
		return nil, err
	}
	endpoint, err := eksEndpoint(ctx, cfg, cluster.ClusterName)
	if err != nil {
		return nil, err
	}

	gen, err := token.NewGenerator(false, false)
	if err != nil {
		return nil, err
	}

	tok, err := gen.GetWithOptions(&token.GetTokenOptions{
		Region:        region,
		ClusterID:     cluster.ClusterName,
		AssumeRoleARN: cluster.RoleArn,
	})
	if err != nil {
		return nil, err
	}

	conf := &rest.Config{
		Host: endpoint,
		TLSClientConfig: rest.TLSClientConfig{
			Insecure: true,
		},
		BearerToken: tok.Token,
	}
	client, err := kubernetes.NewForConfig(conf)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func listUniqueImages(pods []v1.Pod) []string {
	var images []string
	exists := map[string]bool{}
	var containers []v1.Container
	for _, pod := range pods {
		containers = append(containers, pod.Spec.Containers...)
	}
	for _, container := range containers {
		if !exists[container.Image] {
			exists[container.Image] = true
			images = append(images, container.Image)
		}
	}
	return images
}

func inUseImageTags(images []string, repoUri string) []string {
	var result []string
	for _, image := range images {
		if strings.Contains(image, repoUri) {
			s := strings.Split(image, ":")
			if len(s) == 2 {
				result = append(result, s[1])
			} else {
				klog.Errorf("cannot detect tag from image: %v", image)
			}
		}
	}
	return result
}

func deletionCandidateTagsBySinceImagePushed(images []types.ImageDetail, days int) []string {
	var result []string
	deadline := time.Now().Add(-time.Duration(days) * time.Hour * 24)
	for _, image := range images {
		if image.ImagePushedAt.Before(deadline) {
			for _, tag := range image.ImageTags {
				result = append(result, tag)
			}
		}
	}
	return result
}

func deletionCandidateTagsByImageCountMoreThan(images []types.ImageDetail, limit int) []string {
	var result []string
	if len(images) <= limit {
		return result
	}
	sort.Slice(images, func(i, j int) bool { return images[i].ImagePushedAt.After(*images[j].ImagePushedAt) })
	candidates := images[limit:]
	for _, candidate := range candidates {
		for _, tag := range candidate.ImageTags {
			result = append(result, tag)
		}
	}
	return result
}

func decideDeleteTags(candidates, inUses, ignoreRegexes []string) []string {
	var result []string
	var del bool
	for _, candidate := range candidates {
		del = true
		for _, used := range inUses {
			if candidate == used {
				del = false
				break
			}
		}
		if !del {
			continue
		}
		for _, regex := range ignoreRegexes {
			match, err := regexp.Match(regex, []byte(candidate))
			if err != nil {
				klog.Errorf("failed to eval regexp, %v", err)
				continue
			}
			if match {
				del = false
				break
			}
		}
		if del {
			result = append(result, candidate)
		}
	}
	return result
}

func deleteEcrImage(ctx context.Context, cli *ecr.Client, tags []string, name string) error {
	klog.Infof("delete images. Repo: %v, Tags: %v", name, tags)
	var images []types.ImageIdentifier
	for _, tag := range tags {
		t := types.ImageIdentifier{
			ImageTag: aws.String(tag),
		}
		images = append(images, t)
	}
	input := &ecr.BatchDeleteImageInput{
		ImageIds:       images,
		RepositoryName: aws.String(name),
	}
	_, err := cli.BatchDeleteImage(ctx, input)
	return err
}
