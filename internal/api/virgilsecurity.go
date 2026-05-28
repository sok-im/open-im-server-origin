// Copyright © 2026 OpenIM. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package api

import (
	"github.com/gin-gonic/gin"
	pbvirgil "github.com/openimsdk/protocol/virgilsecurity"
	"github.com/openimsdk/tools/a2r"
)

type VirgilSecurityApi struct {
	Client pbvirgil.VirgilSecurityServiceClient
}

func NewVirgilSecurityApi(client pbvirgil.VirgilSecurityServiceClient) VirgilSecurityApi {
	return VirgilSecurityApi{Client: client}
}

func (v *VirgilSecurityApi) IssueVirgilJWT(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.IssueVirgilJWT, v.Client)
}

func (v *VirgilSecurityApi) RegisterDevice(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.RegisterDevice, v.Client)
}

func (v *VirgilSecurityApi) GetDevices(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.GetDevices, v.Client)
}

func (v *VirgilSecurityApi) RevokeDevice(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.RevokeDevice, v.Client)
}

func (v *VirgilSecurityApi) EnsureConversation(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.EnsureConversation, v.Client)
}

func (v *VirgilSecurityApi) SubscribeEvents(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.SubscribeEvents, v.Client)
}

func (v *VirgilSecurityApi) CreateUploadURL(c *gin.Context) {
	a2r.Call(c, pbvirgil.VirgilSecurityServiceClient.CreateUploadURL, v.Client)
}
