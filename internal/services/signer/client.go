package signer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/babylonchain/covenant-signer/internal/utils"
	"github.com/btcsuite/btcd/btcec/v2/schnorr"
	"github.com/btcsuite/btcd/wire"
)

func RequestCovenantSignaure(
	ctx context.Context,
	signerUrl string,
	timeout time.Duration,
	unbondingTx *wire.MsgTx,
) (*schnorr.Signature, error) {
	unbondingTxHex, err := utils.SerializeBTCTxToHex(unbondingTx)

	if err != nil {
		return nil, err
	}

	req := SignUnbondingTxRequest{
		UnbondingTxHex: unbondingTxHex,
	}

	marshalled, err := json.Marshal(req)

	if err != nil {
		return nil, err
	}

	route := fmt.Sprintf("%s/v1/sign-unbonding-tx", signerUrl)

	httpRequest, err := http.NewRequestWithContext(ctx, "POST", route, bytes.NewReader(marshalled))

	if err != nil {
		return nil, err
	}

	// use json
	httpRequest.Header.Set("Content-Type", "application/json")

	client := http.Client{Timeout: timeout}
	// send the request
	res, err := client.Do(httpRequest)

	if err != nil {
		return nil, err
	}

	defer res.Body.Close()
	// read body
	resBody, err := io.ReadAll(res.Body)

	if err != nil {
		return nil, err
	}

	var response PublicResponse[SignUnbondingTxResponse]
	json.Unmarshal(resBody, &response)

	return utils.SchnorSignatureFromHex(response.Data.SignatureHex)
}
