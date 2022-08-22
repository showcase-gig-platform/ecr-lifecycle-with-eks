package main

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	"reflect"
	"testing"
	"time"
)

type ecrImage struct {
	pushedAt time.Time
	tags     []string
	digest   string
}

func ecrImages(images []ecrImage) []types.ImageDetail {
	var result []types.ImageDetail
	for _, image := range images {
		result = append(
			result,
			types.ImageDetail{
				ImagePushedAt: aws.Time(image.pushedAt),
				ImageTags:     image.tags,
				ImageDigest:   aws.String(image.digest),
			},
		)
	}
	return result
}

func Test_listUniqueImages(t *testing.T) {
	type args struct {
		images []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			"unique choice",
			args{
				[]string{
					"targetuser/targetrepo:v1.0.0",
					"targetuser/targetrepo:v1.0.1",
					"targetuser/targetrepo:v1.0.0",
					"targetuser/foorepo:v1.0.0",
					"targetuser/targetrepo:v1.0.0",
					"hogeuser/barrepo:v1.0.0",
				},
			},
			[]string{
				"targetuser/targetrepo:v1.0.0",
				"targetuser/targetrepo:v1.0.1",
				"targetuser/foorepo:v1.0.0",
				"hogeuser/barrepo:v1.0.0",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := listUniqueImages(tt.args.images); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("listUniqueImages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_inUseImageTags(t *testing.T) {
	type args struct {
		images []string
		repo   string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			"get image tags in use",
			args{
				[]string{
					"targetuser/targetrepo:v1.0.0",
					"targetuser/targetrepo:v1.0.1",
					"targetuser/foorepo:v2.0.0",
					"hogeuser/targetrepo:v3.0.0",
				},
				"targetuser/targetrepo",
			},
			[]string{
				"v1.0.0",
				"v1.0.1",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := inUseImageTags(tt.args.images, tt.args.repo); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("inUseImageTags() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deletionCandidateImagesBySinceImagePushed(t *testing.T) {
	type args struct {
		images []types.ImageDetail
		days   int
	}
	tests := []struct {
		name string
		args args
		want deletionImage
	}{
		{
			"get candidates by since image pushed",
			args{
				ecrImages([]ecrImage{
					{
						time.Now().Add(-11*24*time.Hour + -1*time.Hour),
						[]string{
							"11days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-9*24*time.Hour + -1*time.Hour),
						[]string{
							"9days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-10*24*time.Hour + -1*time.Hour),
						[]string{
							"10days",
						},
						"dummydigest",
					},
				}),
				10,
			},
			deletionImage{
				[]string{
					"11days",
					"10days",
				},
				nil,
			},
		},
		{
			"with no tag",
			args{
				ecrImages([]ecrImage{
					{
						time.Now().Add(-11*24*time.Hour + -1*time.Hour),
						[]string{},
						"11daysdigest",
					},
					{
						time.Now().Add(-9*24*time.Hour + -1*time.Hour),
						[]string{},
						"9daysdigest",
					},
					{
						time.Now().Add(-10*24*time.Hour + -1*time.Hour),
						[]string{},
						"10daysdigest",
					},
				}),
				10,
			},
			deletionImage{
				nil,
				[]string{
					"11daysdigest",
					"10daysdigest",
				},
			},
		},
		{
			"tag and digest",
			args{
				ecrImages([]ecrImage{
					{
						time.Now().Add(-9*24*time.Hour + -1*time.Hour),
						[]string{
							"9days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-10*24*time.Hour + -1*time.Hour),
						[]string{
							"10days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-9*24*time.Hour + -1*time.Hour),
						[]string{},
						"9daysdigest",
					},
					{
						time.Now().Add(-10*24*time.Hour + -1*time.Hour),
						[]string{},
						"10daysdigest",
					},
				}),
				10,
			},
			deletionImage{
				[]string{
					"10days",
				},
				[]string{
					"10daysdigest",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deletionCandidateImagesBySinceImagePushed(tt.args.images, tt.args.days); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deletionCandidateImagesBySinceImagePushed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_deletionCandidateImagesByImageCountMoreThan(t *testing.T) {
	type args struct {
		images []types.ImageDetail
		limit  int
	}
	tests := []struct {
		name string
		args args
		want deletionImage
	}{
		{
			"get candidates by image count",
			args{
				ecrImages([]ecrImage{
					{
						time.Now().Add(-1*24*time.Hour + -1*time.Hour),
						[]string{
							"1day",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-3*24*time.Hour + -1*time.Hour),
						[]string{
							"3days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-2*24*time.Hour + -1*time.Hour),
						[]string{
							"2days",
						},
						"dummydigest",
					},
				}),
				2,
			},
			deletionImage{
				[]string{
					"3days",
				},
				nil,
			},
		},
		{
			"with no tag",
			args{
				ecrImages([]ecrImage{
					{
						time.Now().Add(-1*24*time.Hour + -1*time.Hour),
						[]string{},
						"1daydigest",
					},
					{
						time.Now().Add(-3*24*time.Hour + -1*time.Hour),
						[]string{},
						"3daysdigest",
					},
					{
						time.Now().Add(-2*24*time.Hour + -1*time.Hour),
						[]string{},
						"2daysdigest",
					},
				}),
				2,
			},
			deletionImage{
				nil,
				[]string{
					"3daysdigest",
				},
			},
		},
		{
			"tag and digest",
			args{
				ecrImages([]ecrImage{
					{
						time.Now().Add(-3*24*time.Hour + -1*time.Hour),
						[]string{
							"3days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-2*24*time.Hour + -1*time.Hour),
						[]string{
							"2days",
						},
						"dummydigest",
					},
					{
						time.Now().Add(-3*24*time.Hour + -1*time.Hour),
						[]string{},
						"3daysdigest",
					},
					{
						time.Now().Add(-2*24*time.Hour + -1*time.Hour),
						[]string{},
						"2daysdigest",
					},
				}),
				2,
			},
			deletionImage{
				[]string{
					"3days",
				},
				[]string{
					"3daysdigest",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deletionCandidateImagesByImageCountMoreThan(tt.args.images, tt.args.limit); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("deletionCandidateImagesByImageCountMoreThan() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_decideDeleteTags(t *testing.T) {
	type args struct {
		candidates    []string
		inUses        []string
		ignoreRegexes []string
	}
	tests := []struct {
		name string
		args args
		want []string
	}{
		{
			"decide delete tags with inuse",
			args{
				[]string{"v1.0.0", "v1.0.1", "v1.0.2", "stable"},
				[]string{"v1.0.1", "stable"},
				[]string{},
			},
			[]string{"v1.0.0", "v1.0.2"},
		},
		{
			"decide delete tags with regex",
			args{
				[]string{"latest", "stable", "v1-prd", "v1-stg"},
				[]string{},
				[]string{"latest", ".+-prd"},
			},
			[]string{"stable", "v1-stg"},
		},
		{
			"decide delete tags with both",
			args{
				[]string{"latest", "stable", "v1-prd", "v1-stg", "v1.0.0", "v1.0.1"},
				[]string{"v1.0.1", "stable"},
				[]string{"latest", ".+-prd"},
			},
			[]string{"v1-stg", "v1.0.0"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := decideDeleteTags(tt.args.candidates, tt.args.inUses, tt.args.ignoreRegexes); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("decideDeleteTags() = %v, want %v", got, tt.want)
			}
		})
	}
}
