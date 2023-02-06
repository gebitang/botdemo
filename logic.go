package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/MixinNetwork/bot-api-go-client"
	"github.com/MixinNetwork/go-number"
	"github.com/MixinNetwork/mixin/common"
	"github.com/satori/go.uuid"
	"github.com/skip2/go-qrcode"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type (
	Config struct {
		Pin        string `json:"pin"`
		ClientId   string `json:"client_id"`
		SessionId  string `json:"session_id"`
		PinToken   string `json:"pin_token"`
		PrivateKey string `json:"private_key"`
	}

	ImageMessage struct {
		AttachmentID string `json:"attachment_id,omitempty"`
		MimeType     string `json:"mime_type,omitempty"`
		Width        int    `json:"width,omitempty"`
		Height       int    `json:"height,omitempty"`
		Size         int    `json:"size,omitempty"`
		Thumbnail    string `json:"thumbnail,omitempty"`
	}

	Operation struct {
		Id     string
		Type   uint8
		Curve  uint8
		Public string
		Extra  []byte
	}

	mixinBlazeHandler func(ctx context.Context, msg bot.MessageView, clientID string) error

	TrainClient struct {
		Ctx    context.Context
		Config *Config
		Client *bot.BlazeClient
	}
)

func (o *Operation) Encode() []byte {
	pub, err := hex.DecodeString(o.Public)
	if err != nil {
		panic(o.Public)
	}
	enc := common.NewEncoder()
	writeUUID(enc, o.Id)
	writeByte(enc, o.Type)
	writeByte(enc, o.Curve)
	writeBytes(enc, pub)
	writeBytes(enc, o.Extra)
	return enc.Bytes()
}

func writeUUID(enc *common.Encoder, id string) {
	uid := uuid.FromStringOrNil(id)
	enc.Write(uid.Bytes())
}

func writeByte(enc *common.Encoder, b byte) {
	_ = enc.WriteByte(b)
}

func writeBytes(enc *common.Encoder, bytes []byte) {
	_ = enc.WriteByte(byte(len(bytes)))
	enc.Write(bytes)
}

const (
	cnbAssetId = "965e5c6e-434c-3fa9-b780-c50f43cd955c"
	helpMsg    = "\n1. 支持用户查询，请发送 user_id | identity_number\n  2. 支持资产查询，请发送 asset_id | symbol\n  3. 支持每日领取 1cnb，请发送 /claim 或点击签到\n  4. 支持打赏，请发送 /donate 或点击打赏"
)

var (
	mars         *TrainClient
	uploadClient = &http.Client{}
	helpMap      []string
)

func NewClient(ctx context.Context, config *Config) *TrainClient {
	return &TrainClient{
		Ctx:    ctx,
		Config: config,
	}
}

func initHelp() {
	helpMap = []string{"?", "？", "/h", "/H", "/help", "-H", "-h", "--h", "--H"}
}

func (t *TrainClient) HandleClaim(ctx context.Context, userId string) {
	now := time.Now().Format("2006-01-02")
	traceId := bot.UniqueConversationId(userId, now)
	trace, err := bot.ReadTransferByTrace(ctx, traceId, t.Config.ClientId, t.Config.SessionId, t.Config.PrivateKey)
	if err != nil {
		in := &bot.TransferInput{
			AssetId:     cnbAssetId,
			RecipientId: userId,
			Amount:      number.FromString("1"),
			TraceId:     traceId,
			Memo:        "test from bot",
		}

		transfer, e := bot.CreateTransfer(ctx, in, t.Config.ClientId, t.Config.SessionId, t.Config.PrivateKey, t.Config.Pin, t.Config.PinToken)
		if e != nil {
			mErr := &bot.Error{}
			eb, _ := json.Marshal(e)
			json.Unmarshal(eb, mErr)
			// {"status":202,"code":20125,"description":"Transfer has been paid by someone else."}
			if mErr.Code == 20125 {
				t.SendTextMsg(ctx, userId, "keystore已经被其他应用使用")
			}
			// {"status":202,"code":20117,"description":"Insufficient balance."}
			if mErr.Code == 20117 {
				t.SendTextMsg(ctx, userId, "余额不足，请先转账或打赏CNB")
				transferAction := fmt.Sprintf("mixin://transfer/%s", t.Config.ClientId)
				t.Client.SendAppButton(ctx, bot.UniqueConversationId(userId, t.Config.ClientId), userId, "打赏", transferAction, "#1DDA99")
			}
			return
		}
		tt, _ := json.MarshalIndent(transfer, "", "  ")
		fmt.Println("transfer result: ", string(tt))
		return
	}

	if len(trace.SnapshotId) > 0 {
		t.SendTextMsg(ctx, userId, "今日已领取，请明天再来。")
		return
	}
}

func (t *TrainClient) HandleDonate(ctx context.Context, userId string) {
	transferAction := fmt.Sprintf("mixin://transfer/%s", t.Config.ClientId)
	t.Client.SendAppButton(ctx, bot.UniqueConversationId(userId, t.Config.ClientId), userId, "点我打赏", transferAction, "#000000")
}

func (t *TrainClient) AccountProposal(ctx context.Context, userId string) {
	id := uuid.NewV4()
	op := &Operation{
		Id:     id.String(), // as a tranceId
		Type:   110,
		Curve:  1,
		Public: "0390dd88700acf900850b9d7760c4ff52b552c586ae8cbe6dcb9b25343eec95c2d",
	}
	threshold := byte(1)
	total := byte(1)
	geb := "193efbc5-e5df-4a55-9d46-fe1ec4347def"
	owners := []string{geb}
	extra := []byte{threshold, total}
	uid := uuid.FromStringOrNil(owners[0])
	op.Extra = append(extra, uid.Bytes()...)
	memo := base64.RawURLEncoding.EncodeToString(op.Encode())

	fmt.Println(id.String(), " as traceId, memo: ", memo)
	if userId != geb {
		t.SendTextMsg(ctx, userId, "not valid user for proposal. "+memo)
		return
	}

	botIn := &bot.TransferInput{
		AssetId: "c6d0c728-2624-429b-8e0d-d9d19b6592fa",
		Amount:  number.FromFloat(0.0001),
		TraceId: op.Id,
		Memo:    memo,
	}
	botIn.OpponentMultisig.Receivers = []string{
		"71b72e67-3636-473a-9ee4-db7ba3094057", // 7000103394 Calculator
		"148e696f-f1db-4472-a907-ceea50c5cfde", // 7000100092 EOS Mapping
		"c9a9a719-4679-4057-bcf0-98945ed95a81", // 7000100108 Mixin Logs
		"b45dcee0-23d7-4ad1-b51e-c681a257c13e", // 7000103006 Mornin
		"fcb87491-4fa0-4c2f-b387-262b63cbc112", // humanDong
	}
	botIn.OpponentMultisig.Threshold = 4
	transaction, err := bot.CreateMultisigTransaction(ctx, botIn, t.Config.ClientId, t.Config.SessionId, t.Config.PrivateKey, t.Config.Pin, t.Config.PinToken)
	if err != nil {
		fmt.Println("transFailed ", err.Error())
		return
	}
	indent, err := json.MarshalIndent(transaction, "", "  ")
	fmt.Println("transResult: ", string(indent))
	t.SendTextMsg(ctx, userId, "curl https://safe.mixin.dev/accounts/"+id.String())

}

func (t *TrainClient) HelpMsgWithInfo(ctx context.Context, userId, info string) {
	t.SendTextMsg(ctx, userId, info+helpMsg)
	t.Client.SendAppButton(ctx, bot.UniqueConversationId(userId, t.Config.ClientId), userId, "签到", "input:/claim", "#1DDA99")
	t.Client.SendAppButton(ctx, bot.UniqueConversationId(userId, t.Config.ClientId), userId, "打赏", "input:/donate", "#f05d5d")
}

func (t *TrainClient) HandleAssets(ctx context.Context, userId, data string) bool {
	uniqueCid := bot.UniqueConversationId(userId, t.Config.ClientId)
	botMsg := bot.MessageView{
		ConversationId: uniqueCid,
		UserId:         userId,
	}
	if IsValidUUID(data) {
		asset, err := ReadNetworkAsset(ctx, data)
		if err != nil {
			return false
		}
		b, _ := json.MarshalIndent(asset, "", "  ")
		content := fmt.Sprintf("```json\n%s\n```", string(b))
		t.Client.SendPost(ctx, botMsg, content)
	} else {
		assets, err := bot.AssetSearch(ctx, data)
		if err != nil {
			return false
		}
		if len(assets) > 0 {
			t.SendTextMsg(ctx, userId, assets[0].AssetId)
			b, _ := json.MarshalIndent(assets, "", "  ")
			content := fmt.Sprintf("```json\n%s\n```", string(b))
			t.Client.SendPost(ctx, botMsg, content)
		}
	}

	return true
}

func (t *TrainClient) SendTextMsg(ctx context.Context, userId, content string) {
	uniqueCid := bot.UniqueConversationId(userId, t.Config.ClientId)
	t.Client.SendMessage(ctx, uniqueCid, userId, uuid.NewV4().String(), bot.MessageCategoryPlainText, content, "")
}

func (t *TrainClient) HandleUser(ctx context.Context, userId, data string) bool {
	user, err := bot.GetUser(ctx, data, t.Config.ClientId, t.Config.SessionId, t.Config.PrivateKey)
	if err != nil {
		fmt.Println(err)
		return false
	}
	uniqueCid := bot.UniqueConversationId(userId, t.Config.ClientId)

	err = t.Client.SendContact(ctx, uniqueCid, userId, user.UserId)
	if err != nil {
		fmt.Println(err)
		return false
	}
	transferAction := fmt.Sprintf("mixin://transfer/%s", user.UserId)
	label := fmt.Sprintf("\ntransfer to %s\n", user.FullName)
	if data != user.UserId {
		t.SendTextMsg(ctx, userId, user.UserId)
	}

	err = t.Client.SendAppButton(ctx, uniqueCid, userId, label, transferAction, "#1DDA99")
	if err != nil {
		fmt.Println(err)
		return false
	}
	encode, err := qrcode.Encode(transferAction, qrcode.Medium, 256)
	if err != nil {
		fmt.Println(err)
		return false
	}

	attachment, err := bot.CreateAttachment(ctx, t.Config.ClientId, t.Config.SessionId, t.Config.PrivateKey)
	if err != nil {
		fmt.Println(err)
		return false
	}
	err = UploadAttachmentTo(attachment.UploadUrl, encode)
	if err != nil {
		fmt.Println(err)
		return false
	}

	img := &ImageMessage{
		AttachmentID: attachment.AttachmentId,
		MimeType:     "image/jpeg",
		Width:        300,
		Height:       300,
		Size:         len(encode),
		Thumbnail:    base64.StdEncoding.EncodeToString(encode),
	}
	byteImg, err := json.Marshal(img)
	if err != nil {
		fmt.Println(err)
	}
	err = t.Client.SendMessage(ctx, uniqueCid, userId, uuid.NewV4().String(), bot.MessageCategoryPlainImage, string(byteImg), "")
	if err != nil {
		fmt.Println(err)
		return false
	}

	return true
}

func UploadAttachmentTo(uploadURL string, file []byte) error {
	req, err := http.NewRequest("PUT", uploadURL, bytes.NewReader(file))
	if err != nil {
		return err
	}

	req.Header.Add("Content-Type", "application/octet-stream")
	req.Header.Add("x-amz-acl", "public-read")
	req.Header.Add("Content-Length", strconv.Itoa(len(file)))

	resp, err := uploadClient.Do(req)
	if resp != nil {
		_, _ = io.Copy(ioutil.Discard, resp.Body)
		_ = resp.Body.Close()
	}

	if err != nil {
		return err
	}

	if resp.StatusCode >= 300 {
		return errors.New(resp.Status)
	}

	return nil
}

func IsValidUUID(u string) bool {
	_, err := uuid.FromString(u)
	return err == nil
}

func IsNumber(u string) bool {
	_, err := strconv.Atoi(u)
	return err == nil
}

func ReadNetworkAsset(ctx context.Context, name string) (*bot.Asset, error) {
	body, err := bot.Request(ctx, "GET", "/network/assets/"+name, nil, "")
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data  *bot.Asset `json:"data"`
		Error bot.Error  `json:"error"`
	}
	err = json.Unmarshal(body, &resp)
	if err != nil {
		return nil, err
	}
	if resp.Error.Code > 0 {
		return nil, resp.Error
	}
	return resp.Data, nil
}

func isHelpInfo(info string) bool {
	for _, v := range helpMap {
		if v == strings.TrimSpace(info) {
			return true
		}
	}
	return false
}

func handler(ctx context.Context, botMsg bot.MessageView, clientID string) error {
	marshal, _ := json.MarshalIndent(botMsg, "", "  ")
	fmt.Println("msg data: ", string(marshal))
	bytes, _ := base64.StdEncoding.DecodeString(botMsg.Data)
	data := string(bytes)

	if botMsg.Category == bot.MessageCategorySystemAccountSnapshot {
		ss := &bot.Snapshot{}
		json.Unmarshal(bytes, ss)
		asset, _ := ReadNetworkAsset(ctx, ss.AssetId)
		con := fmt.Sprintf("打赏的%s %s 已收到，感谢支持。", ss.Amount, asset.Symbol)
		mars.SendTextMsg(ctx, botMsg.UserId, con)
		return nil
	}

	if botMsg.Category != bot.MessageCategoryPlainText {
		mars.HelpMsgWithInfo(ctx, botMsg.UserId, "仅支持文本信息")
		return nil
	}

	if isHelpInfo(data) {
		mars.HelpMsgWithInfo(ctx, botMsg.UserId, "")
		return nil
	}

	if data == "/claim" {
		mars.HandleClaim(ctx, botMsg.UserId)
		return nil
	}

	if data == "/donate" {
		mars.HandleDonate(ctx, botMsg.UserId)
		return nil
	}

	if data == "/proposal" {
		mars.AccountProposal(ctx, botMsg.UserId)
		return nil
	}

	if IsValidUUID(data) {
		a := mars.HandleAssets(ctx, botMsg.UserId, data)
		b := mars.HandleUser(ctx, botMsg.UserId, data)
		if !a && !b {
			mars.HelpMsgWithInfo(ctx, botMsg.UserId, "指令输入不正确")
		}
		return nil
	}

	if IsNumber(data) {
		a := mars.HandleUser(ctx, botMsg.UserId, data)
		if !a {
			mars.HelpMsgWithInfo(ctx, botMsg.UserId, "指令输入不正确")
		}
		return nil
	} else {
		a := mars.HandleAssets(ctx, botMsg.UserId, data)
		if !a {
			mars.HelpMsgWithInfo(ctx, botMsg.UserId, "指令输入不正确")
		}
	}
	return nil
}

func readConfig(name string) (*Config, error) {
	c := &Config{}
	f, err := os.Open(name)
	// if we os.Open returns an error then handle it
	if err != nil {
		fmt.Println("找不到文件", name, err)
		return c, err
	}
	// defer the closing of our c so that we can parse it later on
	defer f.Close()

	// read our opened c as a byte array.
	byteValue, _ := ioutil.ReadAll(f)

	err = json.Unmarshal(byteValue, &c)
	if err != nil {
		fmt.Println("文件格式错误")
		return c, err
	}
	return c, nil
}
