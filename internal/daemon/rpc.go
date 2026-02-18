package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
)

type Client struct {
	url    string
	user   string
	pass   string
	nextID atomic.Int64
	http   *http.Client
}

func NewClient(url, user, pass string) *Client {
	return &Client{url: url, user: user, pass: pass, http: &http.Client{}}
}

type rpcRequest struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      int64         `json:"id"`
	Method  string        `json:"method"`
	Params  []interface{} `json:"params"`
}

type rpcResponse struct {
	ID     int64           `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("rpc error %d: %s", e.Code, e.Message)
}

func (c *Client) call(method string, params ...interface{}) (json.RawMessage, error) {
	if params == nil {
		params = []interface{}{}
	}
	id := c.nextID.Add(1)
	body, err := json.Marshal(rpcRequest{JSONRPC: "1.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("marshal rpc: %w", err)
	}
	httpReq, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	httpReq.SetBasicAuth(c.user, c.pass)
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read body: %w", err)
	}
	var rpcResp rpcResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal rpc response: %w (body: %s)", err, string(respBody))
	}
	if rpcResp.Error != nil {
		return nil, rpcResp.Error
	}
	return rpcResp.Result, nil
}

type BlockTemplate struct {
	Version           int32   `json:"version"`
	PreviousBlockHash string  `json:"previousblockhash"`
	Transactions      []GBTTx `json:"transactions"`
	CoinbaseAux       CBAux   `json:"coinbaseaux"`
	CoinbaseValue     int64   `json:"coinbasevalue"`
	Target            string  `json:"target"`
	MinTime           int64   `json:"mintime"`
	CurTime           int64   `json:"curtime"`
	Bits              string  `json:"bits"`
	Height            int64   `json:"height"`
}

type GBTTx struct {
	Data string `json:"data"`
	TxID string `json:"txid"`
	Hash string `json:"hash"`
	Fee  int64  `json:"fee"`
}

type CBAux struct {
	Flags string `json:"flags"`
}

func (c *Client) GetBlockTemplate() (*BlockTemplate, error) {
	caps := map[string]interface{}{
		"capabilities": []string{"coinbasetxn", "workid", "coinbase/append"},
		"rules":        []string{"segwit"},
	}
	raw, err := c.call("getblocktemplate", caps)
	if err != nil {
		return nil, err
	}
	var tpl BlockTemplate
	if err := json.Unmarshal(raw, &tpl); err != nil {
		return nil, fmt.Errorf("parse gbt: %w", err)
	}
	return &tpl, nil
}

func (c *Client) SubmitBlock(hexBlock string) error {
	raw, err := c.call("submitblock", hexBlock)
	if err != nil {
		return err
	}
	var reason string
	if err := json.Unmarshal(raw, &reason); err == nil && reason != "" {
		return fmt.Errorf("block rejected: %s", reason)
	}
	return nil
}

func (c *Client) GetBlockCount() (int64, error) {
	raw, err := c.call("getblockcount")
	if err != nil {
		return 0, err
	}
	var height int64
	if err := json.Unmarshal(raw, &height); err != nil {
		return 0, err
	}
	return height, nil
}
