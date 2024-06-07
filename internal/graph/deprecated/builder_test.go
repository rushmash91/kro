package graph

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/aws/symphony/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/yaml"
)

const (
	IRSAResourceGroupSpec = `---
definition:
  spec:
    awsAccountID: string
    eksOIDC: string
    permissionsBoundaryArn: string
    policyArns: '[]string'
    serviceAccountName: string
    resourceConfig:
    deletionPolicy: string
    region: string
    tags: map[string]string`

	IRSAResourceA = `---
name: irsa-policy
definition:
  apiVersion: iam.services.k8s.aws/v1alpha1
  kind: Role
  metadata:
    name: irsa-policy
  spec:
    name: irsa-policy
    policies: ${spec.policyARNs}
`
	IRSAResourceB = `---
name: service-account
definition:
  apiVersion: v1
  kind: ServiceAccount
  metadata:
    name: ${spec.serviceAccountName}
    annotations:
      eks.amazonaws.com/role-arn: ${irsa-policy.status.ACKResourceMetadata.ARN}`

	IRSAResourceC = `---
name: irsa-policy
definition:
  apiVersion: iam.services.k8s.aws/v1alpha1
  kind: Role
  metadata:
    name: irsa-policy
  spec:
	name: irsa-policy
	policies: ${spec.policyARNs}
`
)

func TestBuilder_Build(t *testing.T) {
	var resourcegroupMap map[string]interface{}
	err := yaml.Unmarshal([]byte(IRSAResourceGroupSpec), &resourcegroupMap)
	if err != nil {
		t.Fatalf("couldn't parse yaml data from resource %s: %v", "resourcegroup", err)
	}
	var irsaResourceAMap map[string]interface{}
	err = yaml.Unmarshal([]byte(IRSAResourceA), &irsaResourceAMap)
	if err != nil {
		t.Fatalf("couldn't parse yaml data from resource %s: %v", "irsa-resource-a", err)
	}
	var irsaResourceBMap map[string]interface{}
	err = yaml.Unmarshal([]byte(IRSAResourceB), &irsaResourceBMap)
	if err != nil {
		t.Fatalf("couldn't parse yaml data from resource %s: %v", "irsa-resource-b", err)
	}

	type args struct {
		rawResourceGroup       runtime.RawExtension
		resourcegroupResources []*v1alpha1.Resource
	}
	tests := []struct {
		name    string
		b       *Builder
		args    args
		want    *Collection
		wantErr bool
	}{
		{
			name: "empty variables",
			b:    &Builder{},
			args: args{
				rawResourceGroup:       runtime.RawExtension{},
				resourcegroupResources: []*v1alpha1.Resource{},
			},
			want: &Collection{
				ResourceGroup: &Resource{
					Name:           "main",
					Data:           nil,
					Raw:            []byte(IRSAResourceGroupSpec),
					References:     []*Reference{},
					DependsOn:      []string{},
					ReferenceNames: []string{},
				},
				Resources: []*Resource{},
			},
			wantErr: false,
		},
		{
			name: "resourcegroup only",
			b:    &Builder{},
			args: args{
				rawResourceGroup: runtime.RawExtension{
					Raw: []byte(IRSAResourceGroupSpec),
				},

				resourcegroupResources: []*v1alpha1.Resource{},
			},
			want: &Collection{
				ResourceGroup: &Resource{
					Data:           resourcegroupMap,
					Name:           "main",
					Raw:            []byte(IRSAResourceGroupSpec),
					ReferenceNames: []string{},
					References:     []*Reference{},
					DependsOn:      []string{},
				},
				Resources: []*Resource{},
			},
			wantErr: false,
		},

		{
			name: "two resources with dependency",
			b:    &Builder{},
			args: args{
				rawResourceGroup: runtime.RawExtension{
					Raw: []byte(IRSAResourceGroupSpec),
				},
				resourcegroupResources: []*v1alpha1.Resource{
					{Definition: runtime.RawExtension{Raw: []byte(IRSAResourceA)}, Name: "irsa-policy"},
					{Definition: runtime.RawExtension{Raw: []byte(IRSAResourceB)}, Name: "service-account"},
				},
			},
			want: &Collection{
				ResourceGroup: &Resource{
					Name:           "main",
					Data:           resourcegroupMap,
					Raw:            []byte(IRSAResourceGroupSpec),
					DependsOn:      []string{},
					ReferenceNames: []string{},
					References:     []*Reference{},
				},
				Resources: []*Resource{
					{
						Name: "irsa-policy",
						Data: irsaResourceAMap,
						References: []*Reference{
							{
								Name:              "spec.policyARNs",
								Type:              ReferenceTypeSpec,
								JSONPath:          "policyARNs",
								getTargetResource: nil,
							},
						},
						DependsOn: []string{},
						ReferenceNames: []string{
							"${spec.policyARNs}",
						},
					},
					{
						Name: "service-account",
						Data: irsaResourceBMap,
						References: []*Reference{
							{
								Name:              "spec.serviceAccountName",
								Type:              ReferenceTypeSpec,
								JSONPath:          "serviceAccountName",
								getTargetResource: nil,
							},
							{
								Name:              "irsa-policy.status.ACKResourceMetadata.ARN",
								Type:              ReferenceTypeResource,
								JSONPath:          "status.ACKResourceMetadata.ARN",
								getTargetResource: nil,
							},
						},
						DependsOn: []string{"irsa-policy"},
						ReferenceNames: []string{
							"${spec.serviceAccountName}",
							"${irsa-policy.status.ACKResourceMetadata.ARN}",
						},
					},
				},
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			b := &Builder{}
			got, err := b.Build(tt.args.rawResourceGroup, tt.args.resourcegroupResources)
			if (err != nil) != tt.wantErr {
				t.Errorf("Builder.Build() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// set the getter function to nil to avoid comparing it.
			for i, resource := range got.Resources {
				for j, ref := range resource.References {
					tt.want.Resources[i].References[j].getTargetResource = ref.getTargetResource
				}
			}

			if !reflect.DeepEqual(got.ResourceGroup, tt.want.ResourceGroup) {
				t.Errorf("Builder.Build().ResourceGroup = %+v, want %+v", got, tt.want)
			}
			for i, resource := range got.Resources {
				if !reflect.DeepEqual(got.Resources[i], resource) {
					t.Errorf("Builder.Build().Resource[%d] = %+v, want %+v", i, got.Resources[i], tt.want)
				}
			}

			t.Run("replcements_"+tt.name, func(t *testing.T) {
				reps, err := got.GetReplaceData()
				if (err != nil) != tt.wantErr {
					t.Errorf("Builder.Build() error = %v, wantErr %v", err, tt.wantErr)
					return
				}
				fmt.Println("+++", reps)
			})
		})
	}
}