import { useState, type ReactNode } from "react"
import { Images, Plus, Search, UsersRound } from "lucide-react"

import { useAppSettings } from "@/app-settings"
import type { ViewProps } from "@/admin"
import type { ProfileSlot } from "@/admin/types"
import { Button, Empty, Loading, PageHeader, Panel } from "@/admin/ui"
import { useAdmin, useResource } from "@/admin/use-admin-data"
import { AddDialog, FetchBannerDialog } from "@/admin/views/profile-dialogs"
import { OwnAccountAvatarCard } from "@/admin/views/profile-own-accounts"
import { orderedForSlot, Preview, Candidate } from "@/admin/views/profile-parts"

function CandidateSection({
  title,
  count,
  actions,
  children,
}: {
  title: string
  count: number
  actions?: ReactNode
  children: ReactNode
}) {
  return (
    <section className="adm-profile-section">
      <div className="adm-profile-section-head">
        <h3>
          {title}
          <em className="adm-profile-count">{count}</em>
        </h3>
        {actions ? (
          <div className="adm-profile-section-actions">{actions}</div>
        ) : null}
      </div>
      {children}
    </section>
  )
}

export function ProfileView({ navigate }: ViewProps) {
  const { t } = useAppSettings()
  const { api, act } = useAdmin()
  const media = useResource(() => api.profileMedia(), [])
  const accounts = useResource(() => api.accountAvatars(), [])
  const [addingSlot, setAddingSlot] = useState<ProfileSlot | null>(null)
  const [fetchingBanner, setFetchingBanner] = useState(false)
  const [fetchingYoutube, setFetchingYoutube] = useState(false)

  const state = media.data
  const ownAccounts = (accounts.data?.items ?? []).filter(
    (item) => item.role === "own",
  )
  const ownHandles = ownAccounts.map((item) => item.accountHandle)
  const ownAvatarUrls = new Set(
    ownAccounts.flatMap((account) =>
      account.candidates
        .map((candidate) => candidate.sourceUrl)
        .filter((value): value is string => Boolean(value)),
    ),
  )

  const avatarItems = state
    ? orderedForSlot(
        state.items.filter(
          (item) =>
            item.slot === "avatar" &&
            !item.sourceRef?.startsWith("account-avatar:") &&
            !(
              item.source === "x" &&
              item.sourceUrl &&
              ownAvatarUrls.has(item.sourceUrl)
            ),
        ),
        "avatar",
      )
    : []
  const bannerItems = state
    ? orderedForSlot(
        state.items.filter((item) => item.slot === "banner"),
        "banner",
      )
    : []

  const fetchYoutubeAvatar = async () => {
    setFetchingYoutube(true)
    const next = await act(
      () => api.discoverProfileMedia("youtube", "avatar"),
      "admin.notice.mediaDiscovered",
    )
    setFetchingYoutube(false)
    if (next) media.setData({ items: next.items, active: next.active })
  }

  return (
    <div className="adm-view">
      <PageHeader title={t("admin.profile.title")} />
      {!state ? (
        <Loading />
      ) : (
        <div className="adm-grid">
          <Preview
            state={state}
            onState={media.setData}
            onManageAvatars={() => navigate("avatars")}
          />
          <Panel icon={Images} title={t("admin.profile.candidates")}>
            <div className="adm-profile-sections">
              <CandidateSection
                title={t("admin.profile.section.xAvatars")}
                count={ownAccounts.length}
                actions={
                  <Button
                    size="sm"
                    variant="ghost"
                    icon={UsersRound}
                    onClick={() => navigate("avatars")}
                  >
                    {t("admin.profile.manageAllAvatars")}
                  </Button>
                }
              >
                {ownAccounts.length ? (
                  <div className="adm-candidate-rows">
                    {ownAccounts.map((account) => (
                      <OwnAccountAvatarCard
                        key={`${account.source}:${account.accountKey}`}
                        account={account}
                        activeSiteAvatar={state.active.avatar}
                        onAccountsState={accounts.setData}
                        onProfileState={media.setData}
                      />
                    ))}
                  </div>
                ) : (
                  <Empty title={t("admin.profile.noAccountAvatars")} />
                )}
              </CandidateSection>

              <CandidateSection
                title={t("admin.profile.section.youtubeAvatars")}
                count={avatarItems.length}
                actions={
                  <>
                    <Button
                      size="sm"
                      variant="ghost"
                      icon={Search}
                      busy={fetchingYoutube}
                      onClick={() => void fetchYoutubeAvatar()}
                    >
                      {t("admin.profile.fetchYoutubeAvatar")}
                    </Button>
                    <Button
                      size="sm"
                      icon={Plus}
                      onClick={() => setAddingSlot("avatar")}
                    >
                      {t("admin.profile.addButton")}
                    </Button>
                  </>
                }
              >
                {avatarItems.length ? (
                  <div className="adm-candidate-rows">
                    {avatarItems.map((item) => (
                      <Candidate
                        key={item.id}
                        item={item}
                        onState={media.setData}
                      />
                    ))}
                  </div>
                ) : (
                  <Empty title={t("admin.profile.noYoutubeAvatars")} />
                )}
              </CandidateSection>

              <CandidateSection
                title={t("admin.profile.section.banners")}
                count={bannerItems.length}
                actions={
                  <>
                    <Button
                      size="sm"
                      variant="ghost"
                      icon={Search}
                      onClick={() => setFetchingBanner(true)}
                    >
                      {t("admin.profile.fetchBanner")}
                    </Button>
                    <Button
                      size="sm"
                      icon={Plus}
                      onClick={() => setAddingSlot("banner")}
                    >
                      {t("admin.profile.addButton")}
                    </Button>
                  </>
                }
              >
                {bannerItems.length ? (
                  <div className="adm-candidate-rows">
                    {bannerItems.map((item) => (
                      <Candidate
                        key={item.id}
                        item={item}
                        onState={media.setData}
                      />
                    ))}
                  </div>
                ) : (
                  <Empty title={t("admin.profile.noBanners")} />
                )}
              </CandidateSection>
            </div>
          </Panel>
          {addingSlot ? (
            <AddDialog
              slot={addingSlot}
              onState={media.setData}
              onClose={() => setAddingSlot(null)}
            />
          ) : null}
          {fetchingBanner ? (
            <FetchBannerDialog
              ownHandles={ownHandles}
              onState={media.setData}
              onClose={() => setFetchingBanner(false)}
            />
          ) : null}
        </div>
      )}
    </div>
  )
}
