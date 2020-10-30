/*
Copyright 2020 Gravitational, Inc.

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

package dynamo

import (
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/applicationautoscaling"
	"github.com/aws/aws-sdk-go/service/dynamodb"
	"github.com/gravitational/trace"
)

// getContinuousBackups gets the state of continuous backups.
func (b *Backend) getContinuousBackups(ctx context.Context) (bool, error) {
	resp, err := b.svc.DescribeContinuousBackupsWithContext(ctx, &dynamodb.DescribeContinuousBackupsInput{
		TableName: aws.String(b.TableName),
	})
	if err != nil {
		return false, convertError(err)
	}

	switch *resp.ContinuousBackupsDescription.PointInTimeRecoveryDescription.PointInTimeRecoveryStatus {
	case string(dynamodb.ContinuousBackupsStatusEnabled):
		return true, nil
	case string(dynamodb.ContinuousBackupsStatusDisabled):
		return false, nil
	default:
		return false, trace.BadParameter("dynamo returned unknown state for continuous backups: %v",
			*resp.ContinuousBackupsDescription.PointInTimeRecoveryDescription.PointInTimeRecoveryStatus)
	}
}

// setContinuousBackups sets the state of continuous backups.
func (b *Backend) setContinuousBackups(ctx context.Context) error {
	// Make request to AWS to update continuous backups settings.
	_, err := b.svc.UpdateContinuousBackupsWithContext(ctx, &dynamodb.UpdateContinuousBackupsInput{
		PointInTimeRecoverySpecification: &dynamodb.PointInTimeRecoverySpecification{
			PointInTimeRecoveryEnabled: aws.Bool(b.Config.EnableContinuousBackups),
		},
		TableName: aws.String(b.TableName),
	})
	if err != nil {
		return convertError(err)
	}

	return nil
}

type autoScalingResponse struct {
	readMaxCapacity  int
	readMinCapacity  int
	readTargetValue  float64
	writeMaxCapacity int
	writeMinCapacity int
	writeTargetValue float64
}

// getAutoScaling gets the state of auto scaling.
func (b *Backend) getAutoScaling(ctx context.Context) (*autoScalingResponse, error) {
	svc := applicationautoscaling.New(b.session)

	var resp autoScalingResponse

	// Get scaling targets.
	targetResponse, err := svc.DescribeScalableTargets(&applicationautoscaling.DescribeScalableTargetsInput{
		ServiceNamespace: aws.String("ecs"),
	})
	if err != nil {
		return nil, convertError(err)
	}
	for _, target := range targetResponse.ScalableTargets {
		switch *target.ScalableDimension {
		case applicationautoscaling.ScalableDimensionDynamodbTableReadCapacityUnits:
			resp.readMinCapacity = int(*target.MinCapacity)
			resp.readMaxCapacity = int(*target.MinCapacity)
		case applicationautoscaling.ScalableDimensionDynamodbTableWriteCapacityUnits:
			resp.writeMinCapacity = int(*target.MinCapacity)
			resp.writeMaxCapacity = int(*target.MinCapacity)
		}
	}

	// Get scaling policies.
	policyResponse, err := svc.DescribeScalingPolicies(&applicationautoscaling.DescribeScalingPoliciesInput{
		ServiceNamespace: aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		return nil, convertError(err)
	}
	for _, policy := range policyResponse.ScalingPolicies {
		switch *policy.PolicyName {
		case fmt.Sprintf("%v-%v", b.TableName, readScalingPolicySuffix):
			resp.readTargetValue = *policy.TargetTrackingScalingPolicyConfiguration.TargetValue
		case fmt.Sprintf("%v-%v", b.TableName, writeScalingPolicySuffix):
			resp.writeTargetValue = *policy.TargetTrackingScalingPolicyConfiguration.TargetValue
		}
	}

	return &resp, nil
}

// setAutoScaling sets the state of auto scaling.
func (b *Backend) setAutoScaling(ctx context.Context) error {
	if b.Config.EnableAutoScaling {
		return b.enableAutoScaling(ctx)
	}
	return b.disableAutoScaling(ctx)
}

// enableAutoScaling enables auto scaling.
func (b *Backend) enableAutoScaling(ctx context.Context) error {
	var err error
	svc := applicationautoscaling.New(b.session)

	// Define scaling targets. Defines minimum and maximum {read,write} capacity.
	_, err = svc.RegisterScalableTarget(&applicationautoscaling.RegisterScalableTargetInput{
		MinCapacity:       aws.Int64(int64(b.Config.ReadMinCapacity)),
		MaxCapacity:       aws.Int64(int64(b.Config.ReadMaxCapacity)),
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableReadCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		return convertError(err)
	}
	_, err = svc.RegisterScalableTarget(&applicationautoscaling.RegisterScalableTargetInput{
		MinCapacity:       aws.Int64(int64(b.Config.WriteMinCapacity)),
		MaxCapacity:       aws.Int64(int64(b.Config.WriteMaxCapacity)),
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableWriteCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		return convertError(err)
	}

	// Define scaling policy. Defines the ratio of {read,write} consumed capacity to
	// provisioned capacity DynamoDB will try and maintain.
	_, err = svc.PutScalingPolicy(&applicationautoscaling.PutScalingPolicyInput{
		PolicyName:        aws.String(fmt.Sprintf("%v-%v", b.TableName, readScalingPolicySuffix)),
		PolicyType:        aws.String(applicationautoscaling.PolicyTypeTargetTrackingScaling),
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableReadCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
		TargetTrackingScalingPolicyConfiguration: &applicationautoscaling.TargetTrackingScalingPolicyConfiguration{
			PredefinedMetricSpecification: &applicationautoscaling.PredefinedMetricSpecification{
				PredefinedMetricType: aws.String(applicationautoscaling.MetricTypeDynamoDbreadCapacityUtilization),
			},
			TargetValue: aws.Float64(b.Config.ReadTargetValue),
		},
	})
	if err != nil {
		return convertError(err)
	}
	_, err = svc.PutScalingPolicy(&applicationautoscaling.PutScalingPolicyInput{
		PolicyName:        aws.String(fmt.Sprintf("%v-%v", b.TableName, writeScalingPolicySuffix)),
		PolicyType:        aws.String(applicationautoscaling.PolicyTypeTargetTrackingScaling),
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableWriteCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
		TargetTrackingScalingPolicyConfiguration: &applicationautoscaling.TargetTrackingScalingPolicyConfiguration{
			PredefinedMetricSpecification: &applicationautoscaling.PredefinedMetricSpecification{
				PredefinedMetricType: aws.String(applicationautoscaling.MetricTypeDynamoDbwriteCapacityUtilization),
			},
			TargetValue: aws.Float64(b.Config.WriteTargetValue),
		},
	})
	if err != nil {
		return convertError(err)
	}

	return nil
}

// disableAutoScaling disables auto scaling.
func (b *Backend) disableAutoScaling(ctx context.Context) error {
	var err error
	svc := applicationautoscaling.New(b.session)

	// Delete scaling policy.
	_, err = svc.DeleteScalingPolicy(&applicationautoscaling.DeleteScalingPolicyInput{
		PolicyName:        aws.String(fmt.Sprintf("%v-%v", b.TableName, readScalingPolicySuffix)),
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableReadCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		err = convertError(err)
		if !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}
	}
	_, err = svc.DeleteScalingPolicy(&applicationautoscaling.DeleteScalingPolicyInput{
		PolicyName:        aws.String(fmt.Sprintf("%v-%v", b.TableName, writeScalingPolicySuffix)),
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableWriteCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		err = convertError(err)
		if !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}
	}

	// Delete scaling targets.
	_, err = svc.DeregisterScalableTarget(&applicationautoscaling.DeregisterScalableTargetInput{
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableReadCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		err = convertError(err)
		if !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}
	}
	_, err = svc.DeregisterScalableTarget(&applicationautoscaling.DeregisterScalableTargetInput{
		ResourceId:        aws.String(fmt.Sprintf("%v/%v", resourcePrefix, b.TableName)),
		ScalableDimension: aws.String(applicationautoscaling.ScalableDimensionDynamodbTableWriteCapacityUnits),
		ServiceNamespace:  aws.String(applicationautoscaling.ServiceNamespaceDynamodb),
	})
	if err != nil {
		err = convertError(err)
		if !trace.IsNotFound(err) {
			return trace.Wrap(err)
		}
	}

	return nil
}

const (
	readScalingPolicySuffix  = "read-target-tracking-scaling-policy"
	writeScalingPolicySuffix = "write-target-tracking-scaling-policy"
	resourcePrefix           = "table"
)
