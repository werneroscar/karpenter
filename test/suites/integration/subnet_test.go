/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package integration_test

import (
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"

	"github.com/aws/karpenter/pkg/apis/awsnodetemplate/v1alpha1"
	"github.com/aws/karpenter/pkg/apis/provisioning/v1alpha5"
	awsv1alpha1 "github.com/aws/karpenter/pkg/cloudprovider/aws/apis/v1alpha1"
	"github.com/aws/karpenter/pkg/test"
)

var _ = Describe("Subnets", func() {

	It("should use the subnet-id selector", func() {
		subnets := getSubnets(map[string]string{"karpenter.sh/discovery": env.ClusterName})
		Expect(len(subnets)).ToNot(Equal(0))
		shuffledAZs := lo.Shuffle(lo.Keys(subnets))
		firstSubnet := subnets[shuffledAZs[0]][0]

		provider := test.AWSNodeTemplate(v1alpha1.AWSNodeTemplateSpec{
			AWS: awsv1alpha1.AWS{
				SecurityGroupSelector: map[string]string{"karpenter.sh/discovery": env.ClusterName},
				SubnetSelector:        map[string]string{"aws-ids": firstSubnet},
			},
		})
		provisioner := test.Provisioner(test.ProvisionerOptions{ProviderRef: &v1alpha5.ProviderRef{Name: provider.Name}})
		pod := test.Pod()

		env.ExpectCreated(pod, provider, provisioner)
		env.EventuallyExpectHealthy(pod)
		env.ExpectCreatedNodeCount("==", 1)

		env.ExpectInstance(pod.Spec.NodeName).To(HaveField("SubnetId", HaveValue(Equal(firstSubnet))))
	})

	It("should use a subnet within the AZ requested", func() {
		subnets := getSubnets(map[string]string{"karpenter.sh/discovery": env.ClusterName})
		Expect(len(subnets)).ToNot(Equal(0))
		shuffledAZs := lo.Shuffle(lo.Keys(subnets))

		provider := test.AWSNodeTemplate(v1alpha1.AWSNodeTemplateSpec{
			AWS: awsv1alpha1.AWS{
				SecurityGroupSelector: map[string]string{"karpenter.sh/discovery": env.ClusterName},
				SubnetSelector:        map[string]string{"karpenter.sh/discovery": env.ClusterName},
			},
		})
		provisioner := test.Provisioner(test.ProvisionerOptions{
			ProviderRef: &v1alpha5.ProviderRef{Name: provider.Name},
			Requirements: []v1.NodeSelectorRequirement{
				{
					Key:      v1.LabelZoneFailureDomainStable,
					Operator: "In",
					Values:   []string{shuffledAZs[0]},
				},
			},
		})
		pod := test.Pod()

		env.ExpectCreated(pod, provider, provisioner)
		env.EventuallyExpectHealthy(pod)
		env.ExpectCreatedNodeCount("==", 1)

		env.ExpectInstance(pod.Spec.NodeName).To(HaveField("SubnetId", Or(
			lo.Map(subnets[shuffledAZs[0]], func(subnetID string, _ int) types.GomegaMatcher { return HaveValue(Equal(subnetID)) })...,
		)))
	})
})

// getSubnets returns all subnets matching the label selector
// mapped from AZ -> {subnet-ids...}
func getSubnets(tags map[string]string) map[string][]string {
	var filters []*ec2.Filter
	for key, val := range tags {
		filters = append(filters, &ec2.Filter{
			Name:   aws.String(fmt.Sprintf("tag:%s", key)),
			Values: []*string{aws.String(val)},
		})
	}
	subnets := map[string][]string{}
	err := env.EC2API.DescribeSubnetsPages(&ec2.DescribeSubnetsInput{Filters: filters}, func(dso *ec2.DescribeSubnetsOutput, _ bool) bool {
		for _, subnet := range dso.Subnets {
			subnets[*subnet.AvailabilityZone] = append(subnets[*subnet.AvailabilityZone], *subnet.SubnetId)
		}
		return true
	})
	Expect(err).To(BeNil())
	return subnets
}
