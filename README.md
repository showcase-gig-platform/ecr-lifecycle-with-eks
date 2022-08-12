# ecr-lifecycle-with-eks

`ecr-lifecycle-with-eks` removes expired images in AWS ECR repositories, excluding images in use on your eks clusters.  

## image

### official

`public.ecr.aws/q1m5p9s1/ecr-lifecycle-with-eks` (amd64 linux only)

### build

`$ docker build -t <<your repository>>:<<tag>> .`

## flags

```
-config-file string
    Location of config file. (default "/config.yaml")
-dry-run
    enable dry run (just log tags to be delete)
```

## config

See also `samples/config.yaml`

| Name                   | Required | Description                                                                                              |
|------------------------|----------|----------------------------------------------------------------------------------------------------------|
| region                 | true     | AWS default region in all processes.                                                                     |
| profile                | false    | AWS profile if you need to specify.                                                                      |
| ecr.roleARN            | true     | AWS Role ARN to operate ecr resources.                                                                   |
| ecr.repos              | true     | Target ECR repositories.                                                                                 |
| eks.roleARN            | true     | AWS Role ARN to access eks resource and cluster.                                                         |
| eks.clusterName        | true     | EKS cluster name using images you want to exclude from deletion.                                         |
| commonLifecycle.type   | true     | Base lifecycle. (`sinceImagePushed` or `imageCountMoreThan`)                                             |
| commonLifecycle.number | true     | Base lifecycle value. (units are days for `sinceImagePushed`, number of images for `imageCountMoreThan`) |
| ignoreRegex            | false    | Regex strings to exclude from deletion.                                                                  |

## IAM policy

### base
The execution environment of `ecr-lifecycle-with-eks` needs to be able to assumeRole for two roles below.

### ecr.roleARN

```
ecr:DescribeImages
ecr:DescribeRepositories
ecr:BatchDeleteImage
```

### eks.roleARN

```
eks:DescribeCluster
```

and clusterRole that allows `list pods` in kubernetes cluster.
