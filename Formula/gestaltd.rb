# frozen_string_literal: true

class Gestaltd < Formula
  desc "Gestalt server daemon"
  homepage "https://github.com/valon-technologies/gestalt"
  version "0.0.1-alpha.17"
  license "Apache-2.0"

  on_macos do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.17/gestaltd-macos-arm64.tar.gz"
      sha256 "878d1c1797bd59f548a1c44f9dbe0dba2c40470d01dc8097fce73022d6619259"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.17/gestaltd-macos-x86_64.tar.gz"
      sha256 "7d0198ed1e4c0ec0116e4ffc61e03cadf8ee7c6a398301a9383accc6ebdae6de"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.17/gestaltd-linux-arm64.tar.gz"
      sha256 "155034db57b2079dc063227ff4e0b13a027290d8fda57fa344c5fbb8b2c9c9e6"
    end

    on_intel do
      url "https://github.com/valon-technologies/gestalt/releases/download/gestaltd/v0.0.1-alpha.17/gestaltd-linux-x86_64.tar.gz"
      sha256 "879787aca0586499e5d994bc743bfda0905df3e45839c026efa36ff743fe068d"
    end
  end

  def install
    bin.install "gestaltd"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/gestaltd version")
  end
end
