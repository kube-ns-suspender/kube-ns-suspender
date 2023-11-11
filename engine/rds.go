package engine

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/service/rds"
	"github.com/aws/aws-sdk-go-v2/service/rds/types"
	"github.com/rs/zerolog"

	"strings"
)

func checkRunningRDSClustersConformity(ctx context.Context, l zerolog.Logger, rdsclusters []types.DBCluster, rdsclient *rds.Client, ns string) (bool, error) {
	hasBeenPatched := false
	for _, c := range rdsclusters {
		l.Debug().Str("rdscluster", *c.DBClusterIdentifier).Msgf("running with status %v", *c.Status)
		if c.Status != nil && strings.HasPrefix(*c.Status, "stop") {
			l.Info().Str("rdscluster", *c.DBClusterIdentifier).Msgf("starting rds cluster")
			if err := patchRDSClusterSuspend(ctx, rdsclient, ns, *c.DBClusterIdentifier, false, l); err != nil {
				return hasBeenPatched, err
			}
			hasBeenPatched = true
		}
	}
	return hasBeenPatched, nil
}

func checkSuspendedRDSClustersConformity(ctx context.Context, l zerolog.Logger, rdsclusters []types.DBCluster, rdsclient *rds.Client, ns string) error {
	for _, c := range rdsclusters {
		l.Debug().Str("rdscluster", *c.DBClusterIdentifier).Msgf("suspended with status %v", *c.Status)
		if c.Status != nil && !strings.HasPrefix(*c.Status, "stop") {
			l.Info().Str("rdscluster", *c.DBClusterIdentifier).Msgf("stopping rds cluster")
			if err := patchRDSClusterSuspend(ctx, rdsclient, ns, *c.DBClusterIdentifier, true, l); err != nil {
				return err
			}
		}
	}
	return nil
}

// patchRDSClusterSuspend updates the suspend state of a given rdscluster
func patchRDSClusterSuspend(ctx context.Context, rdsclient *rds.Client, ns, c string, suspend bool, l zerolog.Logger) error {
	var err error
	if suspend {
		_, err = rdsclient.StopDBCluster(ctx, &rds.StopDBClusterInput{DBClusterIdentifier: &c})
		l.Debug().Str("rdscluster", c).Msg("stopped rds cluster")
	} else {
		_, err = rdsclient.StartDBCluster(ctx, &rds.StartDBClusterInput{DBClusterIdentifier: &c})
		l.Debug().Str("rdscluster", c).Msg("started rds cluster")
	}
	return err
}
