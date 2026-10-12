# #29：23个历史分支实质判定

基点 main `38b6e4203d7daf8d622fe10cf57064a84d48ff0d`；关联 issue #29 / Draft PR #30。只读审查，未删除独有历史、未新增生产代码。

分类：2个有价值的独有研究证据、12个功能/补丁已被main替代、8个历史日志/旧发布快照、1个未批准设计。所有原始独有提交仍保留；“可退役”不等于已获得删除许可。

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
| docs/cli-parser-migration-20261004 (`b43c1fa`) | 已被main替代 | main同路径已改为Cobra已实施状态；旧分支仍写等待vendor，没有待整合实现。 #29：先完整归档独有历史；有明确授权后再删。 |
| docs/use-path-resolution (`5e4b118`) | 无法直接判断（未批准设计） | main RFC索引仍明确引用该未批准提案；其字符串use/旧JSON/旧import规则与当前#15静态路径合同不同，不cherry-pick语法。 #13/#29：按当前静态路径合同重新评估提案；不自行加入字符串use或兼容。 |
| docs/workspace-rfc (`594d3a3`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/goxide-toml (`554c095`) | 已被main替代 | 诊断提交554c095与main 5f533d7归一命名后patch-id相同；严格TOML和诊断已存在，旧source来源被#27新schema替代。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/language-site-playground (`2f2177e`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/mod-feasibility (`29d81ec`) | 独有有效工作（引用中的研究证据） | main RFC0021仍引用29d81ec研究；5个研究/fixture路径未进入main，独有证据有价值，保留而非实施未选模块方案。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/result-type-inference (`59d6245`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/rfc-feature-catalog (`46b7caa`) | 已被main替代 | main已有catalog.py、26个RFC和catalog校验；5个拒绝例按.hgo→.gox改名后blob相同，旧静态UI/JSON清单已被#28替代。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-cobra-cli-09f1eb2 (`e21dfc8`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-goxide-naming-d230647 (`16cbda1`) | 历史日志/旧发布快照 | main naming-audit主体保留；文件diff主要为ABI8→9、use/sum2→3和std pin更新，旧版本硬编码不可回迁。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-local-modules-f147455 (`4152dbb`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-source-selection-5d77782 (`6dc779a`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-unified-project-69c1ab0 (`02e003f`) | 历史日志/旧发布快照 | main存在import-go两个输入文件、两个拒绝例、unified RESULTS（命名归一后blob相同）；旧compiler pin/静态UI/来源schema不可回迁。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-workspace-2aa0d06 (`d76ceaa`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/site-workspace-import-27d2e27 (`51405a6`) | 历史日志/旧发布快照 | 独有差异限于website旧版本pin/ABI、历史验证结果及快照集成；main现有project/workspace/source-selection快照与#27/#28已覆盖功能。不回迁过时发布状态。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/target-layout (`8b7bd95`) | 已被main替代 | main仍有OpenLayout/NoSymlinks/ownership，#27增加PublishBundle稳定输出与保护旧文件；不是缺失的target功能。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/unified-project-model (`5d0bb89`) | 已被main替代 | 统一作者模型docs提交5d0bb89与main 8793b7f命名归一后patch-id相同；当前RFC0025及#27覆盖旧TOML设计。 #29：先完整归档独有历史；有明确授权后再删。 |
| feat/version-hgo-features (`2081933`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：先完整归档独有历史；有明确授权后再删。 |
| prep/static-hosting-portability (`0517111`) | 已被main替代 | 全部非merge独有提交有稳定patch-id相同的main对应提交；均被merged #8带入，随后命名/schema/UI演进，不需更新旧基点。 #29：先完整归档独有历史；有明确授权后再删。 |
| research/build-gox-rfc (`4c54b12`) | 独有有效工作（引用中的研究证据） | main RFC0023与build-script-validation仍引用4c54b12；8个原始研究/OS fixture文件不在main，需保存独有研究。严格hermetic旧候选不能当现实现。 #29：先完整归档独有历史；有明确授权后再删。 |
| research/shared-sum-abi (`e39c751`) | 已被main替代 | main研究文档/probe及2个生成快照已存在，2个快照命名归一后blob相同；shared-std-migration明确替代早期建议，当前ABI以内联payload合同为准。 #29：先完整归档独有历史；有明确授权后再删。 |
| review/readability-lowrisk-20261004 (`582d83e`) | 已被main替代 | main保留hasChecks、ExportValidator、短名碰撞检查；96cf5ea/582d83e归一后patch已存在。旧helper改动不再是新增功能。 #29：先完整归档独有历史；有明确授权后再删。 |
| review/t0-realworld-20261004 (`01d5337`) | 历史日志/旧发布快照 | 两处生产修复122caf3/69cdd28归一后patch分别在main 0bba912/9850a46；剩余独有为历史integration/perf/log证据，不回迁旧.hgo fixture。 #29：先完整归档独有历史；有明确授权后再删。 |

## 整合决定

没有找到应重放到当前生产代码的未落地修复；不为更新基点而cherry-pick重复实现。当前有价值的独有内容是main仍引用的研究证据，保留原分支；未批准的use设计交由#13评估。此PR只整合可追溯的判定与下一步，不引入旧schema、兼容层、旧发布pin或机制。
后续删除独有历史必须先生成自包含的可恢复Git归档、独立恢复验证并保存持久交付，然后获取明确授权；本次不依赖reflog或悬空SHA，不创建长期backup分支。
