package keeper

import (
	"github.com/likecoin/likecoin-chain/v5/x/likenft/types"
)

var _ types.QueryServer = Keeper{}
