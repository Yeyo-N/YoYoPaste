import SwiftUI

struct ContentView: View {
    @State private var peers: [Peer] = []
    @State private var enabled = true
    var body: some View {
        VStack {
            HStack {
                Text("YoYoPaste").font(.headline)
                Spacer()
                Circle().fill(enabled ? Color.green : Color.red).frame(width:10, height:10)
                Toggle("", isOn: $enabled).labelsHidden()
                Link("⌗", destination: URL(string: "https://github.com/Yeyo-N/YoYoPaste")!)
            }.padding()
            List(peers, id: \.id) { p in
                HStack {
                    Text(p.name)
                    Spacer()
                    Text(p.ip)
                    Text(p.online ? "online" : "offline")
                    Text(p.lastSync)
                }
            }
        }.task {
            await fetchPeers()
        }
    }
    func fetchPeers() async {
        // Bootstrap from QR (UserDefaults) or default to 100.x:8383
        let addr = UserDefaults.standard.string(forKey: "bootstrapAddr") ?? "100.64.0.1:8383"
        guard let url = URL(string: "http://\(addr)/v0/peers") else { return }
        do {
            let (data, _) = try await URLSession.shared.data(from: url)
            peers = try JSONDecoder().decode([Peer].self, from: data)
        } catch {}
    }
}

struct Peer: Codable, Identifiable {
    let id: String
    let name: String
    let os: String
    let ip: String
    let online: Bool
    let lastSync: String?
    enum CodingKeys: String, CodingKey { case id, name, os, ip, online, lastSync = "last_sync" }
}
