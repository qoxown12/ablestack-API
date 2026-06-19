# Mold CloudStack 자동화 계획

이 문서는 CCVM 생성 이후 CloudStack 초기 구성을 자동화하기 위한 향후 작업 계획을 정리한다. 현재 단계에서는 코드 구현을 진행하지 않고, `mold` 모듈을 어떤 구조로 추가할지와 1차 작업 범위를 명확히 정하는 것을 목표로 한다.

## 배경

현재 Cloud Center 구성 흐름은 다음과 같다.

1. 물리 host에서 스토리지 구성을 완료한다.
2. 수동으로 준비한 qcow2 템플릿을 사용해 CCVM을 생성한다.
3. CCVM에서 CloudStack 서비스를 실행한다.
4. 사용자가 CloudStack 웹에 접속해 비밀번호를 변경한다.
5. 사용자가 zone, pod, cluster, host, primary storage, secondary storage 등을 수동으로 구성한다.

향후 목표는 5번의 수동 작업을 API로 자동화하는 것이다. 최종 사용자는 CloudStack 웹 접속 시 비밀번호 변경 정도만 수행하고, 기본 zone/pod/cluster/host/storage 구성은 이미 완료된 상태를 보는 흐름이 되어야 한다.

## 기본 방향

Mold 자동화는 `CCVM 로컬 API + Cube 올인원 오케스트레이션` 구조로 진행한다.

CloudStack management service는 CCVM 내부에서 동작하므로, 실제 CloudStack API 호출은 CCVM의 `mold` API가 담당한다. 물리 host의 Cube API는 CCVM 생성과 bootstrap 이후 CCVM의 Mold API를 호출하는 오케스트레이터 역할만 수행한다.

권장 구조:

```text
host / Cube deploy-run
  -> ccvm_prepare
  -> ccvm_bootstrap
  -> call CCVM /api/v1/mold/bootstrap
       -> CCVM local CloudStack API
       -> zone/pod/cluster/host/storage ensure
  -> system_profile
```

이 구조를 사용하면 CloudStack 전용 로직이 Cube handler에 섞이지 않고, CCVM에서만 필요한 API를 role 기반으로 분리할 수 있다.

## 패키지 구조

향후 구현 시 아래 구조를 기준으로 한다.

```text
internal/handler/mold
internal/model/mold
internal/service/moldservice
```

| 경로 | 역할 |
| --- | --- |
| `internal/handler/mold` | HTTP endpoint, CCVM role guard, request binding, response 반환 |
| `internal/model/mold` | Mold 요청/응답 DTO, bootstrap step 결과, CloudStack resource 참조 모델 |
| `internal/service/moldservice` | CloudStack API client, resource ensure 로직, async job polling, 입력 기본값 계산 |

`mold`는 CloudStack 자동화 전용 모듈로 두고, Cube는 배포 job에서 `mold_bootstrap` step을 호출하는 정도로만 연결한다.

## Route 정책

Mold API는 CCVM에서만 등록한다. Glue API가 SCVM에서만 route를 등록하는 방식과 같은 정책을 사용한다.

```text
MoldHandler.RegisterRoutesIfCCVM(v1.Group("/mold"))
```

권장 endpoint:

| Method | Endpoint | 역할 |
| --- | --- | --- |
| `GET` | `/api/v1/mold/status` | CloudStack service/API readiness, 현재 zone/pod/cluster/host/storage 요약 조회 |
| `POST` | `/api/v1/mold/bootstrap/plan` | 실제 생성 없이 현재 상태와 생성 예정 작업을 계산 |
| `POST` | `/api/v1/mold/bootstrap` | CloudStack 기본 구성을 idempotent 방식으로 생성/보정 |
| `GET` | `/api/v1/mold/jobs/{job_id}` | 장시간 bootstrap 작업 상태 조회. 1차에서는 필요 시만 구현 |

host와 SCVM에는 `/api/v1/mold` route를 아예 등록하지 않는다. 따라서 잘못된 노드에서 호출하면 404가 발생한다. Swagger도 동일하게 role 기반으로 Mold endpoint를 노출한다.

## 올인원 배포 연결

현재 `/cube/deploy/run`은 `ccvm_bootstrap` 이후 `system_profile`로 종료된다. Mold 자동화가 추가되면 `ccvm_bootstrap`과 `system_profile` 사이에 `mold_bootstrap` step을 추가한다.

권장 순서:

```text
license_apply
cluster_apply
scvm_prepare
scvm_bootstrap
storage_prepare
local_prepare
ccvm_prepare
ccvm_bootstrap
mold_bootstrap
system_profile
```

`mold_bootstrap`의 실행 조건:

| 조건 | 설명 |
| --- | --- |
| CCVM IP 존재 | `clusterConfig.ccvm.ip`가 있어야 한다. |
| CCVM API health 성공 | `/api/v1/health` 응답 이후 Mold 호출을 진행한다. |
| CCVM 라이선스 등록 완료 | `ccvm_bootstrap` 성공 이후 실행한다. |
| CloudStack API readiness 성공 | CCVM 내부에서 CloudStack management API가 응답해야 한다. |
| Mold 요청값 존재 | 자동 구성에 필요한 필수 입력이 없으면 skip 또는 실패 정책을 적용한다. |

`only:["mold_bootstrap"]`으로 단독 재실행할 수 있게 만든다. 단독 재실행 시에도 CCVM health, license status, CloudStack readiness를 먼저 확인한다.

## 자동화 대상

1차 이후 실제 bootstrap에서 다룰 CloudStack 리소스는 아래를 기준으로 한다.

| 순서 | 리소스 | 처리 방향 |
| --- | --- | --- |
| 1 | Zone | 이름으로 조회 후 없으면 생성 |
| 2 | Physical network / traffic type | zone type에 따라 필요한 항목을 생성 또는 확인 |
| 3 | Pod | zone 내부 pod를 이름으로 조회 후 없으면 생성 |
| 4 | Cluster | pod 내부 cluster를 이름으로 조회 후 없으면 생성 |
| 5 | Host | `clusterConfig.hosts[]` 기준으로 KVM host 등록 |
| 6 | Primary storage | 스토리지 구성 결과를 기준으로 primary storage 등록 |
| 7 | Secondary storage / image store | secondary storage URL 기준으로 등록 |
| 8 | System VM 상태 | 필요 시 system VM 생성/시작 상태를 확인 |

CloudStack API 명령 이름은 대상 CloudStack 버전 기준으로 최종 확정한다. 기본 후보는 `listZones/createZone`, `listPods/createPod`, `addCluster`, `addHost`, `createStoragePool`, `addImageStore`, `queryAsyncJobResult`이다.

## Idempotent 원칙

Mold bootstrap은 여러 번 실행해도 중복 생성하지 않아야 한다. 모든 작업은 `create`가 아니라 `ensure` 흐름으로 구현한다.

예시:

```text
ensureZone(name)
  -> listZones(name)
  -> exists: reuse id
  -> missing: createZone

ensurePod(zoneID, name)
  -> listPods(zoneID, name)
  -> exists: reuse id
  -> missing: createPod

ensureCluster(zoneID, podID, name)
  -> listClusters(zoneID, podID, name)
  -> exists: reuse id
  -> missing: addCluster
```

각 step 결과는 아래 상태 중 하나로 기록한다.

| 상태 | 의미 |
| --- | --- |
| `exists` | 이미 존재해서 재사용 |
| `created` | 이번 실행에서 생성 |
| `updated` | 존재하지만 필요한 값을 보정 |
| `skipped` | 입력 또는 환경상 실행하지 않음 |
| `failed` | 실패 |

## 요청 모델 초안

1차 구현 시에는 모든 CloudStack 세부 설정을 열어두기보다, 제품 기본값과 `cluster.json`에서 유추 가능한 값을 최대한 사용한다.

```json
{
  "mode": "apply",
  "admin": {
    "username": "admin",
    "password": "<initial-password>"
  },
  "zone": {
    "name": "ABLESTACK-ZONE",
    "type": "advanced"
  },
  "pod": {
    "name": "ABLESTACK-POD",
    "gateway": "10.10.0.1",
    "netmask": "255.255.0.0",
    "start_ip": "10.10.12.50",
    "end_ip": "10.10.12.100"
  },
  "cluster": {
    "name": "ABLESTACK-CLUSTER",
    "hypervisor": "KVM"
  },
  "host": {
    "username": "root",
    "auth_mode": "ssh_key",
    "ssh_key_path": "/var/cloudstack/management/.ssh/id_rsa.pub"
  },
  "primary_storage": {
    "name": "primary",
    "url": "nfs://<storage-ip>/<path>"
  },
  "secondary_storage": {
    "name": "secondary",
    "url": "nfs://<storage-ip>/<path>"
  }
}
```

보안상 `admin.password`는 `cluster.json`에 평문 저장하지 않는다. 요청 body, 별도 secret 파일, 또는 환경 변수 방식 중 하나를 선택한다. 로그에는 항상 마스킹된 값만 남긴다. Host 등록은 비밀번호보다 CloudStack management SSH key를 host에 사전 배포하는 방식을 우선한다.

## cluster.json 사용 기준

자동화 기본값은 가능한 한 `/etc/ablestack/properties/cluster.json`에서 가져온다.

| cluster.json 경로 | 사용 목적 |
| --- | --- |
| `clusterConfig.type` | HCI, VM, standalone에 따른 자동화 분기 |
| `clusterConfig.ccvm.ip` | CCVM API 호출 대상 및 CloudStack endpoint 계산 |
| `clusterConfig.mngtNic.cidr` | management network 대역 계산 |
| `clusterConfig.mngtNic.gw` | pod gateway 기본값 |
| `clusterConfig.mngtNic.dns` | zone DNS 기본값 |
| `clusterConfig.hosts[]` | CloudStack host 등록 대상 |
| `clusterConfig.hosts[].hostname` | CloudStack host 표시 이름 |
| `clusterConfig.hosts[].ablecube` | host management IP |
| `clusterConfig.hosts[].ablecubePn` | storage/public network 후보 |
| `clusterConfig.storage_network` | storage URL 구성 분기 후보 |

스토리지 URL은 기존 `storage_prepare` 결과와 실제 SCVM/Glue 구성을 함께 봐야 하므로 1차에서는 필수 입력으로 받고, 이후 자동 추론 범위를 넓힌다.

## 비밀번호 처리

CloudStack 초기 구성에는 CloudStack admin 계정 정보가 필요할 수 있다. 이 값은 민감 정보이므로 다음 원칙을 지킨다.

1. `cluster.json`에는 평문 비밀번호를 저장하지 않는다.
2. API log, detail log, job log에는 비밀번호를 마스킹한다.
3. bootstrap 이후 CloudStack admin 비밀번호 변경은 사용자가 CloudStack 웹에서 수행하는 흐름을 유지한다.
4. bootstrap이 완료된 뒤에도 API가 비밀번호에 계속 의존하지 않도록 한다.
5. API key/secret을 생성해 저장해야 하는 경우에는 별도 보안 저장 정책을 먼저 정한다.

## Host 등록 인증 정책

CloudStack host 추가는 사용자/비밀번호 입력보다 SSH key 사전 배포 방식을 우선한다. CCVM에서 CloudStack management 계정이 사용하는 공개키를 각 ablecube host에 등록해두면, zone 구성과 host 추가 단계에서 host 비밀번호를 요청하지 않는 흐름으로 갈 수 있다.

예시:

```bash
ssh-copy-id -i /var/cloudstack/management/.ssh/id_rsa.pub ablecube1
ssh-copy-id -i /var/cloudstack/management/.ssh/id_rsa.pub ablecube2
ssh-copy-id -i /var/cloudstack/management/.ssh/id_rsa.pub ablecube3
```

운영 기준:

| 항목 | 정책 |
| --- | --- |
| 기본 host 인증 방식 | `ssh_key` |
| 기본 공개키 경로 | `/var/cloudstack/management/.ssh/id_rsa.pub` |
| 배포 대상 | `clusterConfig.hosts[]`의 ablecube host |
| 검증 방법 | CCVM에서 CloudStack management 계정 기준으로 각 host에 무비밀번호 SSH 가능 여부 확인 |
| fallback | SSH key 방식이 불가능한 환경에서만 host username/password 입력을 허용 |

따라서 Mold 요청 모델에서 `host.password`는 기본 필수값으로 두지 않는다. `bootstrap/plan` 단계는 host별 SSH key 접근 가능 여부를 먼저 확인하고, 실패한 host는 `missing_ssh_key` 또는 `ssh_unreachable` 같은 상태로 반환한다.

## 1차 작업 범위

1차는 실제 CloudStack 리소스 생성보다 안전한 골격과 상태 확인을 먼저 만든다.

### 1차 목표

| 항목 | 설명 |
| --- | --- |
| Mold package 골격 | `handler/model/service` 구조 생성 |
| CCVM role guard | CCVM에서만 `/api/v1/mold` route 등록 |
| Swagger tag | `Mold-Status`, `Mold-Bootstrap` 정도로 분리 |
| status API | CloudStack service/API readiness와 현재 resource summary 반환 |
| bootstrap plan API | 생성 예정 작업을 계산하지만 실제 생성은 하지 않음 |
| deploy-run step 정의 | `mold_bootstrap` step 이름과 요청 모델만 추가 |
| host SSH key 검증 | CCVM에서 ablecube host로 CloudStack management SSH key 접근 가능 여부 확인 |
| 문서화 | 올인원 배포 가이드에 향후 step 위치와 사용 조건 정리 |

1차에서는 `POST /api/v1/mold/bootstrap`의 실제 생성 로직을 열지 않거나, `dry_run=true`만 허용하는 방식이 안전하다.

### 1차 완료 기준

1. host/SCVM에서는 `/api/v1/mold`가 등록되지 않는다.
2. CCVM에서는 `/api/v1/mold/status`가 등록된다.
3. CloudStack management API가 내려가 있으면 status가 명확한 실패 원인을 반환한다.
4. `/api/v1/mold/bootstrap/plan`이 zone/pod/cluster/host/storage에 대해 `exists`, `would_create`, `missing_input`, `missing_ssh_key`를 구분해 반환한다.
5. `/cube/deploy/run`에서 `mold_bootstrap` step을 선택할 수 있지만, 실제 apply는 2차 이후로 제한할 수 있다.
6. 민감 정보는 응답과 로그에서 마스킹된다.

## 2차 작업 범위

2차는 CloudStack API client와 공통 실행기를 만든다.

| 항목 | 설명 |
| --- | --- |
| CloudStack client | login/session 또는 apiKey/secret 방식 지원 |
| request signer | apiKey/secret 방식이 필요하면 서명 생성 구현 |
| async polling | `queryAsyncJobResult` 기반 job 완료 대기 |
| response parser | CloudStack API 응답을 공통 구조로 변환 |
| error mapping | CloudStack 오류를 Mold API 응답 코드/메시지로 변환 |
| unit test | client URL 생성, masking, async polling 상태 변환 테스트 |

이 단계까지 완료되면 실제 리소스 생성 없이 CloudStack API와 안정적으로 통신할 수 있어야 한다.

## 3차 작업 범위

3차는 idempotent resource ensure 로직을 구현한다.

| 순서 | 작업 |
| --- | --- |
| 1 | `ensureZone` |
| 2 | `ensurePhysicalNetwork` / `ensureTrafficType` |
| 3 | `ensurePod` |
| 4 | `ensureCluster` |
| 5 | `ensureHost` |
| 6 | `ensurePrimaryStorage` |
| 7 | `ensureSecondaryStorage` |
| 8 | `status summary` 보강 |

각 ensure 함수는 `plan`과 `apply`에서 같은 판단 로직을 사용한다. `plan`은 실행하지 않고 결과만 반환하고, `apply`는 누락된 항목만 생성한다.

## 4차 작업 범위

4차는 Cube 올인원과 실제로 연결한다.

| 항목 | 설명 |
| --- | --- |
| `DeployRunStepMoldBootstrap` 추가 | `ccvm_bootstrap` 다음 step |
| `DeployRunRequest.Mold` 추가 | Mold bootstrap 요청 payload |
| CCVM 호출 | host Cube에서 CCVM `/api/v1/mold/bootstrap` 호출 |
| retry 정책 | CCVM API health와 CloudStack readiness retry |
| systemProfile flag | 성공 시 `bootstrap.mold` 또는 `bootstrap.cloudstack` 반영 |
| deploy status | 올인원 UI에서 Mold 단계 상태 표시 |

flag 이름은 제품 용어를 우선하면 `bootstrap.mold`, 기술 명확성을 우선하면 `bootstrap.cloudstack`이 적합하다. 구현 전에 하나로 확정한다.

## 5차 작업 범위

5차는 올인원 UI에 반영한다.

| 항목 | 설명 |
| --- | --- |
| 입력 단계 추가 | CloudStack admin 초기 비밀번호, host SSH key 상태, storage URL |
| 유효성 검사 | IP/CIDR/range/storage URL/필수값 검증 |
| plan 표시 | 생성 예정 항목과 이미 존재하는 항목을 UI에서 구분 |
| apply 실행 | `/cube/deploy/run`의 `mold_bootstrap`으로 연결 |
| 실패 표시 | step별 실패 원인과 재실행 가능 범위 표시 |

UI는 사용 설명 문구를 늘리기보다 입력값과 검증 결과 중심으로 구성한다.

## 6차 테스트 범위

실제 테스트는 CloudStack이 올라간 CCVM 환경에서 진행한다.

| 테스트 | 확인 내용 |
| --- | --- |
| role guard | host/SCVM에서 Mold route 미등록, CCVM에서 등록 |
| status | CloudStack service down/up 상태 구분 |
| plan | 기존 리소스와 생성 예정 리소스 구분 |
| apply 신규 구성 | 빈 CloudStack 환경에서 zone/pod/cluster/host/storage 생성 |
| apply 재실행 | 중복 생성 없이 `exists`로 처리 |
| 실패 복구 | 중간 실패 후 재실행 시 완료된 step 재사용 |
| 로그 | 비밀번호와 secret 마스킹 |
| deploy-run | `ccvm_bootstrap -> mold_bootstrap -> system_profile` 순서 확인 |

## 확정이 필요한 항목

구현 전에 아래 값을 제품 기본값으로 정해야 한다.

| 항목 | 필요 이유 |
| --- | --- |
| CloudStack 버전 | API 파라미터와 응답 구조 확정 |
| zone type | Basic/Advanced에 따라 network 자동화가 달라짐 |
| zone/pod/cluster 기본 이름 | UI 입력 최소화를 위한 기본값 |
| host 등록 방식 | CloudStack `addHost`에 필요한 URL과 SSH key 기반 등록 정책 |
| primary storage 종류 | NFS, Ceph, iSCSI 등 URL 형식 결정 |
| secondary storage 종류 | image store 등록 방식 결정 |
| system VM template 상태 | 이미 템플릿에 포함되어 있는지 확인 |
| CloudStack admin 초기 계정 | bootstrap 인증 방식 결정 |
| bootstrap 완료 flag 이름 | `bootstrap.mold` 또는 `bootstrap.cloudstack` 중 선택 |

## 권장 진행 순서

1. 이 문서를 기준으로 제품 기본값을 확정한다.
2. `mold` package 골격과 CCVM route guard를 만든다.
3. `/api/v1/mold/status`와 `/api/v1/mold/bootstrap/plan`만 먼저 구현한다.
4. CloudStack API client와 async job polling을 구현한다.
5. zone부터 storage까지 ensure 함수를 순서대로 구현한다.
6. `mold_bootstrap`을 `/cube/deploy/run`에 연결한다.
7. 올인원 UI에 plan/apply 단계를 붙인다.
8. 실제 CCVM 환경에서 신규 구성, 재실행, 중간 실패 복구를 검증한다.

1차에서는 "생성"보다 "확인, 계획, 안전한 연결"을 우선한다. 이 기준을 지키면 실제 운영 환경 테스트 전에 코드 구조와 입력 모델을 안정적으로 만들 수 있다.
