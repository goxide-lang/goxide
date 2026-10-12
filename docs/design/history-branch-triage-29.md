# #29：23个历史分支实质判定

基点 main `38b6e4203d7daf8d622fe10cf57064a84d48ff0d`；关联 issue #29 / Draft PR #30。判定后按用户授权完成可恢复清理；未新增生产代码。

分类：2个有价值的独有研究证据、12个功能/补丁已被main替代、8个历史日志/旧发布快照、1个未批准设计。20个已替代/旧发布分支现已归档并删除；2个引用中研究和1个未批准设计仍保留。

## 优先：ahead=1

| 分支 | ahead/behind | 判定与依据 |
|---|---|---|
| docs/use-path-resolution | 1/173 | 无法直接判断（未批准设计）：main RFC索引仍明确引用该未批准提案；其字符串use/旧JSON/旧import规则与当前#15静态路径合同不同，不cherry-pick语法。 |
| feat/mod-feasibility | 1/196 | 独有有效工作（引用中的研究证据）：main RFC0021仍引用29d81ec研究；5个研究/fixture路径未进入main，独有证据有价值，保留而非实施未选模块方案。 |
| feat/rfc-feature-catalog | 1/173 | 已被main替代：main已有catalog.py、26个RFC和catalog校验；5个拒绝例按.hgo→.gox改名后blob相同，旧静态UI/JSON清单已被#28替代。 |
| feat/site-goxide-naming-d230647 | 1/122 | 历史日志/旧发布快照：main naming-audit主体保留；文件diff主要为ABI8→9、use/sum2→3和std pin更新，旧版本硬编码不可回迁。 |
| feat/site-unified-project-69c1ab0 | 1/149 | 历史日志/旧发布快照：main存在import-go两个输入文件、两个拒绝例、unified RESULTS（命名归一后blob相同）；旧compiler pin/静态UI/来源schema不可回迁。 |
| feat/target-layout | 1/184 | 已被main替代：main仍有OpenLayout/NoSymlinks/ownership，#27增加PublishBundle稳定输出与保护旧文件；不是缺失的target功能。 |
| prep/static-hosting-portability | 1/186 | 已被main替代：全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 |
| research/shared-sum-abi | 1/173 | 已被main替代：main研究文档/probe及2个生成快照已存在，2个快照命名归一后blob相同；shared-std-migration明确替代早期建议，当前ABI以内联payload合同为准。 |

## 五个稳定patch-id候选

11个非merge独有提交全部在main历史找到相同stable patch-id；对应提交均已进入merged PR #8，并可从#27/#28 merge commit到达。完整tip文件树不等于当前main，原因包含后续命名、schema、ABI和UI演进；不据此声称整个旧树可直接合并。

- `docs/workspace-rfc`：`594d3a3` → main `2b4f349`；`be41fba` → main `ae7b2be`；`61d4734` → main `f1a5691`；`5c7192c` → main `16a37d3`。
- `feat/language-site-playground`：`2f2177e` → main `3143d5f`；`bcc1834` → main `038f467`。
- `feat/result-type-inference`：`59d6245` → main `44635d7`；`bf365b9` → main `861d763`。
- `feat/version-hgo-features`：`2081933` → main `16098cc`；`25e3fa6` → main `4b1c029`。
- `prep/static-hosting-portability`：`0517111` → main `0996213`。

## 全部23个分支

| 分支（tip） | 类别 | 最小依据与下一步 |
|---|---|---|
| docs/cli-parser-migration-20261004 (`b43c1fa`) | 已被main替代 | main同路径已改为Cobra已实施状态；旧分支仍写等待vendor，没有待整合实现。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| docs/use-path-resolution (`5e4b118`) | 无法直接判断（未批准设计） | main RFC索引仍明确引用该未批准提案；其字符串use/旧JSON/旧import规则与当前#15静态路径合同不同，不cherry-pick语法。 #13/#29：按当前静态路径合同重新评估提案；不自行加入字符串use或兼容。 |
| docs/workspace-rfc (`594d3a3`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/goxide-toml (`554c095`) | 已被main替代 | 诊断提交554c095与main 5f533d7归一命名后patch-id相同；严格TOML和诊断已存在，旧source来源被#27新schema替代。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/language-site-playground (`2f2177e`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/mod-feasibility (`29d81ec`) | 独有有效工作（引用中的研究证据） | main RFC0021仍引用29d81ec研究；5个研究/fixture路径未进入main，独有证据有价值，保留而非实施未选模块方案。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/result-type-inference (`59d6245`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/rfc-feature-catalog (`46b7caa`) | 已被main替代 | main已有catalog.py、26个RFC和catalog校验；5个拒绝例按.hgo→.gox改名后blob相同，旧静态UI/JSON清单已被#28替代。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-cobra-cli-09f1eb2 (`e21dfc8`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-goxide-naming-d230647 (`16cbda1`) | 历史日志/旧发布快照 | main naming-audit主体保留；文件diff主要为ABI8→9、use/sum2→3和std pin更新，旧版本硬编码不可回迁。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-local-modules-f147455 (`4152dbb`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-source-selection-5d77782 (`6dc779a`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-unified-project-69c1ab0 (`02e003f`) | 历史日志/旧发布快照 | main存在import-go两个输入文件、两个拒绝例、unified RESULTS（命名归一后blob相同）；旧compiler pin/静态UI/来源schema不可回迁。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-workspace-2aa0d06 (`d76ceaa`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/site-workspace-import-27d2e27 (`51405a6`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/target-layout (`8b7bd95`) | 已被main替代 | main仍有OpenLayout/NoSymlinks/ownership，#27增加PublishBundle稳定输出与保护旧文件；不是缺失的target功能。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/unified-project-model (`5d0bb89`) | 已被main替代 | 统一作者模型docs提交5d0bb89与main 8793b7f命名归一后patch-id相同；当前RFC0025及#27覆盖旧TOML设计。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| feat/version-hgo-features (`2081933`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| prep/static-hosting-portability (`0517111`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| research/build-gox-rfc (`4c54b12`) | 独有有效工作（引用中的研究证据） | main RFC0023与build-script-validation仍引用4c54b12；8个原始研究/OS fixture文件不在main，需保存独有研究。严格hermetic旧候选不能当现实现。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| research/shared-sum-abi (`e39c751`) | 已被main替代 | main研究文档/probe及2个生成快照已存在，2个快照命名归一后blob相同；shared-std-migration明确替代早期建议，当前ABI以内联payload合同为准。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| review/readability-lowrisk-20261004 (`582d83e`) | 已被main替代 | main保留hasChecks、ExportValidator、短名碰撞检查；96cf5ea/582d83e归一后patch已存在。旧helper改动不再是新增功能。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |
| review/t0-realworld-20261004 (`01d5337`) | 历史日志/旧发布快照 | 两处生产修复122caf3/69cdd28归一后patch分别在main 0bba912/9850a46；剩余独有为历史integration/perf/log证据，不回迁旧.hgo fixture。 #29：已完成自包含归档、Library回下载恢复验证和授权删除。 |

## 整合决定

没有找到应重放到当前生产代码的未落地修复；不为更新基点而cherry-pick重复实现。当前有价值的独有内容是main仍引用的研究证据，保留研究原分支；未批准的use设计交由#13评估。此PR只整合可追溯的判定与下一步，不引入旧schema、兼容层、旧发布pin或机制。
归档及删除按末节记录完成；仍保留的研究/待决设计未获删除授权，不作清理。本次不依赖reflog或悬空SHA，不创建长期backup分支。

## 已执行：完整归档与可恢复删除

用户明确授权继续清理。bundle `goxide-retired-20-branches-20261012.bundle` 自包含、无外部前置历史；3,505,349字节，SHA-256 `088322dd2ee1c7e9d1bc41657d21467e8565fe31f6d46aca6fd06915218a8a03`。

归档Library ID：`libfile_58352f997ad481918c5e34843927f6fd`。干净临时bare仓恢复20个tip一致，4,469条对象记录一致，fsck --full --strict通过；Library回下载后摘要与全20 tip/fsck再次通过。

删除前核对tip未前进、open PR head/base、在运行Actions、protected标志及无Pages/部署工作流。普通原子删除20个远端分支，无force；当前剩余9个（原始68删除60，再加本次Draft分支1个）。本地无对应head，仅清理失效tracking refs，工作树文件未变。

恢复：下载bundle后，在空目录执行：

```sh
git init --bare recovered.git
git -C recovered.git fetch /absolute/path/goxide-retired-20-branches-20261012.bundle 'refs/remotes/origin/*:refs/heads/*'
git -C recovered.git fsck --full --strict
# 如需恢复某个远端分支（只创建，不force）：
git -C recovered.git push https://github.com/goxide-lang/goxide.git refs/heads/BRANCH:refs/heads/BRANCH
```

BRANCH须替换为表中准确分支名；原始tip及完整对象均在bundle中，不依赖reflog、悬空SHA或长期backup分支。

## 快照转为用途tag

用户后续授权快照用archive/snapshot/checkpoint前缀；不为无价值重复补丁建tag。7个历史发布状态保存为snapshot/，T0独有性能/验证记录保存为checkpoint/。原分支已删除，无重建分支。

仓库仅有CI，push过滤main；8个原tip无GitHub Actions工作流，仓库无Pages。检查现有tag后创建annotated tag，普通原子推送并读回peeled SHA全部一致；未创建release或部署。

| 原分支 | 新tag | 原tip SHA |
|---|---|---|
| feat/site-cobra-cli-09f1eb2 | snapshot/site-cobra-cli-09f1eb2 | e21dfc8de42eacc1d6e2c4a1504cbc382a135250 |
| feat/site-goxide-naming-d230647 | snapshot/site-goxide-naming-d230647 | 16cbda175f2b9bc12e92f58c2f667122827520ae |
| feat/site-local-modules-f147455 | snapshot/site-local-modules-f147455 | 4152dbb9fe9ac865921d5b97e54d83099651af9d |
| feat/site-source-selection-5d77782 | snapshot/site-source-selection-5d77782 | 6dc779a651af3eb34121f53637a55bfb6feee387 |
| feat/site-unified-project-69c1ab0 | snapshot/site-unified-project-69c1ab0 | 02e003f74ea097db172c4c7920c67adb8c4b7f5f |
| feat/site-workspace-2aa0d06 | snapshot/site-workspace-2aa0d06 | d76ceaabad43ae308c6d577e63281f666a61e26f |
| feat/site-workspace-import-27d2e27 | snapshot/site-workspace-import-27d2e27 | 51405a6982e45167c732fb01906e2b666b39fcdd |
| review/t0-realworld-20261004 | checkpoint/t0-realworld-20261004 | 01d53377777113321b315a35673f77735f372ad0 |

其余12个已替代分支不逐个建tag；完整原始历史仍在已验证Library bundle，当前远端分支仍9个。
