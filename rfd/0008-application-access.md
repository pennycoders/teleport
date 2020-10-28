---
authors: Russell Jones (rjones@gravitational.com)
state: draft
---

# RFD 8 - Teleport Application Access

## What

This document contains technical implementation details of Teleport Application Access.

## Why

## Use Cases

The initial implementation of Teleport Application Access is targeted at users that would like to expose internal applications and dashboards on the public internet.

## Details

### Identity Headers

As described in a previous section, Teleport uses TLS mutual authentication to pass identity information between internal components. However, identity information is passed to proxied applications in the form of a signed JWT in a request header named `teleport-jwt-assertion`.

This identity information can be used to show the identity of the user currently logged in as well as change the state of the internal application. For example, because Teleport roles are forwarded to proxied applications within the JWT header, an control panel application could show an regular or admin view based on the Teleport identity of the user.

#### Issuance

All Teleport clusters have a User and Host CA used to issue user and host SSH and TLS certificates. Teleport Application Access introduces a JWT signer to each cluster to issue JWTs. The JWT signer uses 2048-bit RSA keys similar to the existing CAs.

#### Verification

An unauthenticated endpoint at `https://proxy.example.com:3080/.well-known/jwks.json` returns the public keys that can be used to verify the signed JWT. Multiple keys are supported because JWT signers can be rotated similar to CAs.

The keys are in the [JWK](https://tools.ietf.org/html/rfc7517) format. Example output of this endpoint is below.

```
$ curl https://proxy.example.com:3080/.well-known/jwks.json | jq .
{
  "keys": [
    {
      "kty": "rsa",
      "alg": "RS256",
      "n": "xk-0VSVZY76QGqeN9TD-FJp32s8jZrpsalnRoFwlZ_JwPbbd5-_bPKcz8o2tv1eJS0Ll6ePxRCyK68Jz2UC4V4RiYaqJCRq_qVpDQMB1sQ7p9M-8qvT82FJ-Rv-W4RNe3xRmBSFDYdXaFm51Uk8OIYfv-oZ0kGptKpkNY390aJOzjHPH2MqSvhk9Xn8GwM8kEbpSllavdJCRPCeNVGJXiSCsWrOA_wsv_jqBP6g3UOA9GnI8R6HR14OxV3C184vb3NxIqxtrW0C4W6UtSbMDcKcNCgajq2l56pHO8In5GoPCrHqlo379LE5QqpXeeHj8uqcjeGdxXTuPrRq1AuBpvQ",
      "e": "AQAB"
    }
  ]
}
```

While the RFC explains the format of these keys and how parse and verify a JWT token, we strongly recommend you use a well written and supported library in the language of your choice to validate the JWT and you not try to write parsing and validation code yourself.

We have provided an example within Teleport on how to [validate the JWT token](https://github.com/gravitational/teleport/blob/a7fca6bf2bc15fc487620b410151c2108ac1d6cc/lib/jwt/jwt.go#L192-L225) written in Go.

#### Claims

The JWT embeds within it claims about the identity of the subject and issuer of the token.

The following public claims are included:

* `aud`: Audience of JWT. This is the URI of the proxied application to which the request is being forwarded.
* `exp`: Expiration time of the JWT. This value is always in sync with the expiration of the TLS certificate.
* `iss`: Issuer of the JWT. This value is the name of the Teleport cluster issuing the token.
* `nbf`: Not before time of the JWT. This is the time at which the JWT becomes valid.
* `sub`: Subject of the JWT. This is the Teleport identity of the user to whom the JWT was issued.

The following private claims are included.

* `username`: Similar to sub. This is the Teleport identity of the user to whom the JWT was issued.
* `roles`: List of Teleport roles assigned to the user.

#### Rotation

The JWT signing keys are rotated along with User and Host CAs when using the `tctl auth rotate [...]` command. If you specifically only want to rotate your JWT signer, use the `--type=jwt` flag.

#### Example

The following header will be sent to an internal application:

```
teleport-jwt-assertion: eyJhbGciOiJSUzI1NiIsInR5cCI6IkpXVCJ9.eyJhdWQiOlsiaHR0cDovL2xvY2FsaG9zdDo4MDgwL2FwcCJdLCJleHAiOjE2MDM1Mjk5NzEsImlzcyI6ImV4YW1wbGUuY29tIiwibmJmIjoxNjAzNDg2Nzg2LCJyb2xlcyI6WyJhZG1pbiJdLCJzdWIiOiJyam9uZXMiLCJ1c2VybmFtZSI6InJqb25lcyJ9.SnyYMyjjcxEUsPnf-WxWy33yVWsHR3hQPCml-fizX1HJY7jojkKroPbrXBCO-WEJ8RqCzv0j6u1pz_PllNPhhPrCE8Q32WAB2OaazVM2FHxsiEVyMInCUVEAsrieYo0BQidXTj85yGgEPV45VdbnqWdJSzVr1UmUF6kDdMwhS3Zyr-SRAZw9ix_jBK6nxDmlD0TgJh9eAvRhbjvxU12I6A4VqZVPTrefoWsdZTHrYvg2oqztHtNSycqsbqfIBnNmg__opWKgouW_t-Xv58aA8scW_5DavVitPhQBbsPH0QRKfu-xMNDtmfa6eBAKe7E9uO2uDcmDA26dHKIA2n90Gw
```

Decoding to the below JSON.

```
{
  "aud": [
    "http://localhost:8080/app"
  ],
  "exp": 1603530049,
  "iss": "example.com",
  "nbf": 1603486842,
  "roles": [
    "admin"
  ],
  "sub": "foo",
  "username": "foo"
}
```

### Logout

Each application the user launches maintains its own session. Sessions automatically TTL out after the time specified on the role and certificate.

To explicitly logout a session, an authenticated session can issue a `GET /teleport-logout` or a `DELETE /teleport-logout` request.

Internal applications and implementers are encouraged to support `DELETE /teleport-logout` in the form of a logout button within the internal application.

The `GET /teleport-logout` endpoint is for internal applications that can not be modified.
