import { CleanupList } from '@/components/CleanupList'
import { ControllerSection } from '@/components/ControllerSection'
import { Header } from '@/components/Header'
import { QuestionList } from '@/components/QuestionList'
import { ReturnedList } from '@/components/ReturnedList'
import { NightBlock, ParkedBlock, QueueBlock, RunningBlock } from '@/components/RunBlocks'
import { Summary } from '@/components/Summary'
import { PrioritiesTab } from '@/components/priorities/PrioritiesTab'
import { Tabs, useHashTab } from '@/components/Tabs'
import { UsagePanel } from '@/components/UsagePanel'

export default function App() {
  const [tab, setTab] = useHashTab()
  return (
    <main className={`mx-auto space-y-6 p-6 ${tab === 'priorities' ? 'max-w-[1180px]' : 'max-w-3xl'}`}>
      <Header />
      <UsagePanel />
      <Tabs active={tab} onChange={setTab} />
      {tab === 'controller' ? (
        <ControllerSection>
          <Summary />
          <QuestionList />
          <ReturnedList />
          <RunningBlock />
          <ParkedBlock />
          <QueueBlock />
          <NightBlock />
          <CleanupList />
        </ControllerSection>
      ) : (
        <PrioritiesTab />
      )}
    </main>
  )
}
