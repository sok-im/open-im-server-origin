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
	pbopenmls "github.com/openimsdk/protocol/openmls"
	"github.com/openimsdk/tools/a2r"
)

type OpenMLSApi struct {
	Client pbopenmls.OpenMLSServiceClient
}

func NewOpenMLSApi(client pbopenmls.OpenMLSServiceClient) OpenMLSApi {
	return OpenMLSApi{Client: client}
}

// ---------- KeyPackage ----------

func (o *OpenMLSApi) UploadKeyPackage(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.UploadKeyPackage, o.Client)
}

func (o *OpenMLSApi) GetKeyPackages(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.GetKeyPackages, o.Client)
}

func (o *OpenMLSApi) GetKeyPackageCount(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.GetKeyPackageCount, o.Client)
}

func (o *OpenMLSApi) RefreshKeyPackages(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.RefreshKeyPackages, o.Client)
}

// ---------- Group Commit ----------

func (o *OpenMLSApi) SubmitCommit(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.SubmitCommit, o.Client)
}

func (o *OpenMLSApi) GetCommits(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.GetCommits, o.Client)
}

func (o *OpenMLSApi) SendWelcome(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.SendWelcome, o.Client)
}

func (o *OpenMLSApi) GetGroupState(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.GetGroupState, o.Client)
}

func (o *OpenMLSApi) DeleteGroup(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.DeleteGroup, o.Client)
}

// ---------- Credential ----------

func (o *OpenMLSApi) IssueCredential(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.IssueCredential, o.Client)
}

func (o *OpenMLSApi) VerifyCredential(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.VerifyCredential, o.Client)
}

func (o *OpenMLSApi) GetRootPublicKey(c *gin.Context) {
	a2r.Call(c, pbopenmls.OpenMLSServiceClient.GetRootPublicKey, o.Client)
}
