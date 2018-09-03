package transfer

import (
	"math/big"
	"reflect"

	"github.com/likecoin/likechain/abci/account"
	"github.com/likecoin/likechain/abci/context"
	handler "github.com/likecoin/likechain/abci/handlers"
	logger "github.com/likecoin/likechain/abci/log"
	"github.com/likecoin/likechain/abci/response"
	"github.com/likecoin/likechain/abci/types"
	"github.com/likecoin/likechain/abci/utils"
	"github.com/sirupsen/logrus"
)

var log = logger.L

func logTx(tx *types.TransferTransaction) *logrus.Entry {
	return log.WithField("tx", tx)
}

func checkTransfer(state context.IImmutableState, rawTx *types.Transaction) response.R {
	tx := rawTx.GetTransferTx()
	if tx == nil {
		log.Panic("Expect TransferTx but got nil")
	}

	if !validateTransferTransactionFormat(state, tx) {
		logTx(tx).Info(response.TransferCheckTxInvalidFormat.Info)
		return response.TransferCheckTxInvalidFormat
	}

	senderID := account.IdentifierToLikeChainID(state, tx.From)
	if senderID == nil {
		logTx(tx).Info(response.TransferCheckTxSenderNotRegistered.Info)
		return response.TransferCheckTxSenderNotRegistered
	}

	if !validateTransferSignature(state, tx) {
		logTx(tx).Info(response.TransferCheckTxInvalidSignature.Info)
		return response.TransferCheckTxInvalidSignature
	}

	nextNonce := account.FetchNextNonce(state, *senderID)
	if tx.Nonce > nextNonce {
		logTx(tx).Info(response.TransferCheckTxInvalidNonce.Info)
		return response.TransferCheckTxInvalidNonce
	} else if tx.Nonce < nextNonce {
		logTx(tx).Info(response.TransferCheckTxDuplicated.Info)
		return response.TransferCheckTxDuplicated
	}

	senderBalance := account.FetchBalance(state, *senderID)
	total := tx.Fee.ToBigInt()
	for _, target := range tx.ToList {
		receiverID := account.IdentifierToLikeChainID(state, target.To)
		if receiverID == nil {
			logTx(tx).Info(response.TransferCheckTxReceiverNotRegistered.Info)
			return response.TransferCheckTxReceiverNotRegistered
		}

		amount := target.Value.ToBigInt()
		total.Add(total, amount)
		if senderBalance.Cmp(total) < 0 {
			logTx(tx).
				WithField("receiver_id", receiverID.ToString()).
				Info(response.TransferCheckTxNotEnoughBalance.Info)
			return response.TransferCheckTxNotEnoughBalance
		}
	}

	return response.Success
}

func deliverTransfer(state context.IMutableState, rawTx *types.Transaction) response.R {
	tx := rawTx.GetTransferTx()
	if tx == nil {
		log.Panic("Expect TransferTx but got nil")
	}

	if !validateTransferTransactionFormat(state, tx) {
		logTx(tx).Info(response.TransferDeliverTxInvalidFormat.Info)
		return response.TransferDeliverTxInvalidFormat
	}

	senderID := account.IdentifierToLikeChainID(state, tx.From)
	if senderID == nil {
		logTx(tx).Info(response.TransferDeliverTxSenderNotRegistered.Info)
		return response.TransferDeliverTxSenderNotRegistered
	}

	if !validateTransferSignature(state, tx) {
		logTx(tx).Info(response.TransferDeliverTxInvalidSignature.Info)
		return response.TransferDeliverTxInvalidSignature
	}

	nextNonce := account.FetchNextNonce(state, *senderID)
	if tx.Nonce > nextNonce {
		logTx(tx).Info(response.TransferDeliverTxInvalidNonce.Info)
		return response.TransferDeliverTxInvalidNonce
	} else if tx.Nonce < nextNonce {
		logTx(tx).Info(response.TransferDeliverTxDuplicated.Info)
		return response.TransferDeliverTxDuplicated
	}

	senderBalance := account.FetchBalance(state, *senderID)
	total := tx.Fee.ToBigInt()
	transfers := make(map[*types.LikeChainID]*big.Int, len(tx.ToList))
	for _, target := range tx.ToList {
		receiverID := account.IdentifierToLikeChainID(state, target.To)
		if receiverID == nil {
			logTx(tx).Info(response.TransferDeliverTxReceiverNotRegistered.Info)
			return response.TransferDeliverTxReceiverNotRegistered
		}

		amount := target.Value.ToBigInt()
		total.Add(total, amount)
		if senderBalance.Cmp(total) < 0 {
			logTx(tx).
				WithField("receiver_id", receiverID.ToString()).
				Info(response.TransferDeliverTxNotEnoughBalance.Info)
			return response.TransferDeliverTxNotEnoughBalance
		}

		transfers[receiverID] = amount
	}

	for receiverID, amount := range transfers {
		account.AddBalance(state, receiverID, amount)
	}
	account.MinusBalance(state, senderID, total)

	account.IncrementNextNonce(state, *senderID)

	return response.Success
}

func validateTransferSignature(state context.IImmutableState, tx *types.TransferTransaction) bool {
	hashedMsg := tx.GenerateSigningMessageHash()
	sigAddr, err := utils.RecoverSignature(hashedMsg, tx.Sig)
	if err != nil {
		log.WithError(err).Info("Unable to recover signature when validating signature")
		return false
	}

	senderAddr := tx.From.GetAddr()
	if senderAddr != nil {
		if senderAddr.ToEthereum() == sigAddr {
			return true
		}
		log.WithFields(logrus.Fields{
			"txAddr":  senderAddr.ToHex(),
			"sigAddr": sigAddr.Hex(),
		}).Info("Recovered address is not match")
	} else {
		id := tx.From.GetLikeChainID()
		if id != nil {
			if account.IsLikeChainIDHasAddress(state, id, &sigAddr) {
				return true
			}
			log.WithFields(logrus.Fields{
				"likeChainID": id.ToString(),
				"sigAddr":     sigAddr.Hex(),
			}).Info("Recovered address is not bind to the LikeChain ID of the sender")
		}
	}

	return false
}

func validateTransferTransactionFormat(state context.IImmutableState, tx *types.TransferTransaction) bool {
	if !tx.From.IsValidFormat() {
		log.Debug("Invalid sender format in transfer transaction")
		return false
	}

	if len(tx.ToList) > 0 {
		for _, target := range tx.ToList {
			if !target.IsValidFormat() {
				log.Debug("Invalid receiver format in transfer transaction")
				return false
			}
		}
	} else {
		log.Debug("No receiver in transfer transaction")
		return false
	}

	if !tx.Sig.IsValidFormat() {
		log.Debug("Invalid signature format in transfer transaction")
		return false
	}

	return true
}

func init() {
	t := reflect.TypeOf((*types.Transaction_TransferTx)(nil))
	handler.RegisterCheckTxHandler(t, checkTransfer)
	handler.RegisterDeliverTxHandler(t, deliverTransfer)
}