# eTLD+1（可注册域）技术说明

inhomo 归类域名时的匹配粒度。本文说明 eTLD+1 是什么、为什么不能靠「取后两段」、
以及 inhomo 为什么用它把连接的目的 host 归约到「可注册域」再查追踪器表。

> 相关代码：`internal/tracker/tracker.go`（`Classify` 用 `golang.org/x/net/publicsuffix`
> 的 `EffectiveTLDPlusOne`）。相关文档：[`tracker-radar-data.md`](./tracker-radar-data.md)。
> 数据基准：[Public Suffix List](https://publicsuffix.org/)（Mozilla 维护）。

## 1. 一句话定义

**eTLD+1 = 可注册的那个域名**——你去域名商能买到的那一级。名字拆开看：
**eTLD**（effective Top-Level Domain，有效顶级域）**+ 1**（再往前取一个标签）。

## 2. 为什么叫「有效」顶级域

- 真正的 TLD（顶级域）是最后一段：`.com`、`.cn`、`.uk`。
- 但有些**多段后缀**，任何人都能在其下注册子域，实际上「相当于」顶级域——例如
  `.co.uk`、`.com.cn`、`.github.io`（GitHub Pages）、`.vercel.app`。这些叫**有效顶级域（eTLD）**，
  由 **Public Suffix List（公共后缀列表，PSL）** 枚举维护。
- **eTLD 再往前取一段 = eTLD+1 = 可注册域。**

换句话说：eTLD 是「大家共享、不属于某个人」的后缀；eTLD+1 才是「某个主体注册下来、归它所有」的那一级。

## 3. 例子

| host | eTLD | **eTLD+1（可注册域）** |
|---|---|---|
| `www.google.com` | `.com` | **google.com** |
| `graph.facebook.com` | `.com` | **facebook.com** |
| `a.b.google-analytics.com` | `.com` | **google-analytics.com** |
| `www.bbc.co.uk` | `.co.uk` | **bbc.co.uk**（不是 `co.uk`，更不是 `uk`） |
| `shop.example.com.cn` | `.com.cn` | **example.com.cn** |
| `myblog.github.io` | `.github.io` | **myblog.github.io** |

## 4. inhomo 为什么用它

追踪器表（[`tracker-radar.json`](./tracker-radar-data.md)）的键是**可注册域**，如 `google-analytics.com`。
但一条连接的目的 host 可能是 `www.google-analytics.com`、`region1.google-analytics.com`……
inhomo 先把 host **归约到 eTLD+1**（`google-analytics.com`）再查表，好处：

- **同一域名的所有子域命中同一条**——这正是「这个域名归谁」的正确粒度（归属是按可注册域走的）。
- 不必把每个子域都收进表；表按可注册域组织，命中率高、体量小。

## 5. 为什么不能简单「取后两段」

因为 `.co.uk` 这类多段后缀的坑：`www.bbc.co.uk` 取后两段是 `co.uk`——那是**公共后缀**、不属于任何人，
不是「谁的域名」。正确的可注册域是 `bbc.co.uk`。判对与否**必须查公共后缀列表**，光靠数点、切字符串会错。

所以 inhomo 用 `golang.org/x/net/publicsuffix`（Go 官方的 PSL 实现）的 `EffectiveTLDPlusOne(host)`
来取 eTLD+1，而不是自己 `strings.Split` 后两段。

## 6. 边界：没有 eTLD+1 的情况

有些 host 取不到 eTLD+1，`EffectiveTLDPlusOne` 会**返回错误**：

- **IP 字面量**：`1.2.3.4`、`[2408:…]::1`（IP 不是域名，没有可注册域的概念）
- **单标签主机**：`localhost`（缺一段有效后缀）
- **空串**

inhomo 对这些一律归「**未知**」、**绝不报错**（见 `Classify`）。这也是为什么直连 IP 的连接不会被当追踪器归类——它压根没有域名归属可查。

## 7. 相关

- [Public Suffix List](https://publicsuffix.org/)：eTLD 的权威清单，Mozilla 维护、各浏览器/库共用。
- `golang.org/x/net/publicsuffix`：Go 的 PSL 实现，inhomo 用它取 eTLD+1。
- [`tracker-radar-data.md`](./tracker-radar-data.md)：追踪器归属表——inhomo 就是用 eTLD+1 去查它。
