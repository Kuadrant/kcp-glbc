# Custom Domain Support

Authors: Craig Brookes @maleck13

Epic: https://github.com/Kuadrant/kcp-glbc/issues/80

Date: 11th May 2022


## Job Stories

- As a developer, I want to use my own domain for the APIs I expose so that I can provide my APIs under a well known and trusted domain.


## Goals

- Ensure supporting use of custom domains at the workspace level is done in a safe way
- Enable using the GLBC managed domain as a CNAME on KCP registered clusters within the context of a KCP workspace
- Allow sub domains of a verified custom domain to be used within the context of the KCP workspace without needing additional verification.

## Non Goals

- Consider domain transfer and rechecking ownership once initial ownership has been verified
- Org level control over custom domains allowed beyond RBAC within a workspace.
- Workspaces that are shared by multiple untrusted users

## Current Approach

GLBC's default behaviour is to not allow the use of custom domain names. It assumes that many workspaces my share infrastructure and that one or all workspaces sharing infrastructure cannot be trusted.


## Proposed Solution

There are two forms of domain within the context of the global load balancer:
1) a managed domain: this is a domain that the glbc owns the DNS for and is used for handing out hosts such as (guid).manageddomain.com
2) a custom domain: This is a domain owned by the end user that they intend to use for accessing their applications.

We assume individuals within a single workspace are considered trusted. So proving ownership of a domain will happen at the workspace level.
To prove ownership of a domain we will use DNS validation via a txt record. This is the same process used for things like lets encrypt and GH pages. The assumption is that if you can make a change to the DNS Zone of a given domain, then you have control over where that domain resolves and so are considered the owner or at least authorised to use the domain.
Ingress objects are our main API and is how application developers are used to expressing their intent around ingress. In the below flow we use the presence of a custom domain in the Ingress as intent to use that domain. 
Before we can allow the use of that domain, we want the end user to prove ownership of the domain. 
 

Flow:

- End user creates an Ingress resource with a custom domain in the host field 
- Mutating webhook for Ingress sees custom domain 
- Mutating webhook for Ingress checks for a DomainVerification resource in the target workspace (outlined below), 
  - If not present:
    - strip the custom domain and any tls section and add them as annotations to the Ingress object
    - create a DomainVerification resource
  - If present and not validated, 
    - it will strip out the custom domain and any associated tls section and adds them to an annotation.
  - If present and validated:
    - allow the change through un-modified
- Validating webhook for the DomainVerification resource, will ensure only the glbc service account can modify the DomainVerification resource.
- GLBC controller will watch for DomainVerification resource and add a token when it sees one 
- During reconcile, it will perform a DNS check to see has the token been added as a txt record. 
- Status will be updated appropriately. 
- Once a successful check is completed, GLBC controller takes the removed host and tls section from the annotation and reapplies them to the Ingress. 
- Both the GLBC host and the Custom Domain are synced to the physical cluster with their tls secrets
- GLBC controller during reconcile of the DomainVerification will check for any ingresses using the 

To support this we will add a new workspace level API that will be exported by GLBC for use in end user workspaces. End users will only be allowed to view this resource. 

```yaml 
apiVersion: kuadrant.dev/v1alpha1
kind: DomainVerification
metadata:
  name: my-domain
spec:
  domain: mydomain.com
status:
  token: <generated by controller based on org and workspace># this value is protected by the DomainVerification validating webhook  
  conditions: # reflect the state of verification but are not used to prove verification
    - lastTransitionTime: "2019-10-22T16:29:24Z"
      status: "False"
      domain: mydomain.com
      message: "domain failed DNS verification"
      nextVerification: "2019-10-22T16:23:24Z"
      type: Verified  
    - lastTransitionTime: "2019-10-22T16:29:24Z"
      status: "True"
      domain: myotherdomain.com
      message: "domain successfully verified"
      nextVerification: "2019-10-22T16:29:24Z"
      type: Verified        
```

If for some reason the webhook protecting this resource is unavailable, we will configure a failure policy of ```fail``` https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/#failure-policy    


# Clean  Up 

Any Ingress this DomainVerification applies to will be set as the owner of the object. When all OwnerRefs are removed the DomainVerification will also be removed

## Testing

We will want to design an e2e test for this feature however this will be complex as it involves checking a DNSRecord. We may want to look into something like local stack https://github.com/localstack/localstack


## Checklist

- [ x ] An epic has been created and linked to
- [ x ] Reviewers have been added. It is important that the right reviewers are selected. 