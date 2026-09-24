import { useEffect, useMemo, useState } from 'react'
import { Button, Form, Input, Modal, Popconfirm, Select, Table, Tag } from 'antd'
import { Lock, Plus, RefreshCw, ShieldCheck, Unlock, XCircle } from 'lucide-react'
import { PageHeader } from '../components/common/PageHeader'
import { useAuth } from '../hooks/useAuth'
import { useFreezeStore } from '../stores/freezeStore'
import { useTankStore } from '../stores/tankStore'
import { freezeStatusLabels, type FreezeInput, type FreezeStatus, type PeriodFreeze } from '../types/freeze'
import { dateTime, localInputDate } from '../utils/format'

const statusColor: Record<FreezeStatus, string> = {
  pending_approval: 'processing',
  active: 'error',
  rejected: 'default',
  released: 'success'
}

const statusFilters: { value: FreezeStatus; label: string }[] = [
  { value: 'pending_approval', label: '待复核' },
  { value: 'active', label: '生效中' },
  { value: 'rejected', label: '已驳回' },
  { value: 'released', label: '已解除' }
]

export function FreezesPage() {
  const { can } = useAuth()
  const store = useFreezeStore()
  const tanks = useTankStore()
  const [statusFilter, setStatusFilter] = useState<FreezeStatus | undefined>()
  const [createOpen, setCreateOpen] = useState(false)
  const [rejectTarget, setRejectTarget] = useState<PeriodFreeze | null>(null)
  const [releaseTarget, setReleaseTarget] = useState<PeriodFreeze | null>(null)
  const [createForm] = Form.useForm<FreezeInput>()
  const [noteForm] = Form.useForm<{ note: string }>()

  useEffect(() => { void Promise.all([store.load(undefined, statusFilter), tanks.load()]) }, [statusFilter])

  const tankName = useMemo(() => {
    const names = new Map(tanks.items.map((tank) => [tank.id, tank.tank_code + ' · ' + tank.name]))
    return (item: PeriodFreeze) => item.tank?.tank_code ?? names.get(item.tank_id) ?? item.tank_id
  }, [tanks.items])

  const openCreate = () => {
    const end = new Date()
    const start = new Date(end.getTime() - 24 * 3_600_000)
    createForm.setFieldsValue({
      tank_id: tanks.items[0]?.id,
      period_start: localInputDate(start),
      period_end: localInputDate(end),
      freeze_note: ''
    })
    setCreateOpen(true)
  }

  const create = async (values: FreezeInput) => {
    await store.create({
      ...values,
      period_start: new Date(values.period_start).toISOString(),
      period_end: new Date(values.period_end).toISOString()
    })
    setCreateOpen(false)
    createForm.resetFields()
  }

  const submitNote = async ({ note }: { note: string }) => {
    if (rejectTarget) {
      await store.reject(rejectTarget, note)
      setRejectTarget(null)
    } else if (releaseTarget) {
      await store.release(releaseTarget, note)
      setReleaseTarget(null)
    }
    noteForm.resetFields()
  }

  return (
    <>
      <PageHeader
        eyebrow="PERIOD FREEZE"
        title="期间冻结"
        description="平衡结果送审后冻结储罐计量期间；生效期内补录快照、新增转移及确认/取消转移一律挡下并回报冻结编号。"
        actions={
          <>
            <Select
              allowClear
              placeholder="全部状态"
              style={{ width: 132 }}
              options={statusFilters}
              value={statusFilter}
              onChange={(value) => setStatusFilter(value)}
            />
            <Button icon={<RefreshCw size={16} />} onClick={() => void store.load(undefined, statusFilter)}>刷新</Button>
            {can('process_analyst', 'admin') && <Button type="primary" icon={<Plus size={16} />} onClick={openCreate}>申请冻结</Button>}
          </>
        }
      />
      <section className="data-panel">
        <div className="section-heading">
          <h2>冻结记录</h2>
          <span>{store.items.filter((item) => item.freeze_status === 'active').length} 条生效中</span>
        </div>
        <Table<PeriodFreeze>
          rowKey="id"
          loading={store.loading}
          dataSource={store.items}
          pagination={{ pageSize: 12, showSizeChanger: false }}
          scroll={{ x: 1180 }}
          columns={[
            { title: '编号', dataIndex: 'id', width: 80, render: (id: number) => <Tag icon={<Lock size={12} />}>#{id}</Tag> },
            { title: '储罐', key: 'tank', width: 150, render: (_, item) => tankName(item) },
            { title: '开始', dataIndex: 'period_start', width: 130, render: dateTime },
            { title: '结束', dataIndex: 'period_end', width: 130, render: dateTime },
            { title: '状态', dataIndex: 'freeze_status', width: 100, render: (value: FreezeStatus) => <Tag color={statusColor[value]}>{freezeStatusLabels[value]}</Tag> },
            { title: '冻结说明', dataIndex: 'freeze_note', ellipsis: true },
            {
              title: '复核 / 解除', key: 'decision', width: 200,
              render: (_, item) => (
                <div className="freeze-meta">
                  {item.decided_at && <span>复核：{dateTime(item.decided_at)}{item.decision_note ? ` · ${item.decision_note}` : ''}</span>}
                  {item.released_at && <span>解除：{dateTime(item.released_at)} · {item.release_reason}</span>}
                </div>
              )
            },
            {
              title: '操作', key: 'action', fixed: 'right', width: 200,
              render: (_, item) => can('reviewer', 'admin') ? (
                <div className="table-actions">
                  {item.freeze_status === 'pending_approval' && (
                    <Popconfirm title="通过后该期间立即冻结，确认通过？" onConfirm={() => void store.approve(item)}>
                      <Button size="small" type="text" icon={<ShieldCheck size={15} />}>通过</Button>
                    </Popconfirm>
                  )}
                  {item.freeze_status === 'pending_approval' && (
                    <Button size="small" type="text" danger icon={<XCircle size={15} />} onClick={() => setRejectTarget(item)}>驳回</Button>
                  )}
                  {item.freeze_status === 'active' && (
                    <Button size="small" type="text" icon={<Unlock size={15} />} onClick={() => setReleaseTarget(item)}>解除冻结</Button>
                  )}
                </div>
              ) : null
            }
          ]}
        />
      </section>

      <Modal title="申请储罐期间冻结" open={createOpen} onCancel={() => setCreateOpen(false)} footer={null} destroyOnClose>
        <Form<FreezeInput> form={createForm} layout="vertical" onFinish={create} requiredMark={false}>
          <div className="form-grid">
            <Form.Item className="span-2" name="tank_id" label="储罐" rules={[{ required: true }]}>
              <Select options={tanks.items.map((tank) => ({ value: tank.id, label: tank.tank_code + ' · ' + tank.name }))} />
            </Form.Item>
            <Form.Item name="period_start" label="期间开始" rules={[{ required: true }]}><Input type="datetime-local" /></Form.Item>
            <Form.Item name="period_end" label="期间结束" rules={[{ required: true }]}><Input type="datetime-local" /></Form.Item>
            <Form.Item className="span-2" name="freeze_note" label="冻结说明（平衡结果编号、送审情况等）" rules={[{ required: true, min: 3, max: 500 }]}>
              <Input.TextArea rows={3} />
            </Form.Item>
          </div>
          <Button type="primary" htmlType="submit" block>提交复核</Button>
        </Form>
      </Modal>

      <Modal
        title={releaseTarget ? '解除期间冻结' : '驳回冻结申请'}
        open={Boolean(rejectTarget || releaseTarget)}
        onCancel={() => { setRejectTarget(null); setReleaseTarget(null); noteForm.resetFields() }}
        footer={null}
        destroyOnClose
      >
        <Form form={noteForm} layout="vertical" onFinish={submitNote} requiredMark={false}>
          <Form.Item
            name="note"
            label={releaseTarget ? '解除原因（不少于 6 个字符，将记录操作者与时间）' : '驳回说明（不少于 6 个字符）'}
            rules={[{ required: true, min: 6, max: 500 }]}
          >
            <Input.TextArea rows={3} />
          </Form.Item>
          <Button type="primary" danger htmlType="submit" block>{releaseTarget ? '确认解除' : '确认驳回'}</Button>
        </Form>
      </Modal>
    </>
  )
}
